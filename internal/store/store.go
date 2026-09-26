package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"curio/internal/auth"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ db *sql.DB }
type User struct {
	ID       int64
	Username string
	Settings Settings
}
type Settings struct {
	DisplayName, Theme, HomeLabel string
	Latitude, Longitude           sql.NullFloat64
	Interests                     []string
}
type Session struct {
	User User
	CSRF string
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(filepath.Join(dir, "curio.db"))
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err = s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL DEFAULT (unixepoch()))`); err != nil {
		return err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	if version > len(files) {
		return fmt.Errorf("database schema %d is newer than supported schema %d", version, len(files))
	}
	for n, file := range files {
		next := n + 1
		if next <= version {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", file.Name(), err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(?)`, next); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// EnterUser creates a household profile on first use, or opens its existing identity.
func (s *Store) EnterUser(ctx context.Context, username string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO users(username) VALUES(?) ON CONFLICT(username) DO NOTHING`, username); err != nil {
		return 0, err
	}
	var id int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username=?`, username).Scan(&id); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_settings(user_id,display_name) VALUES(?,?) ON CONFLICT(user_id) DO NOTHING`, id, username); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
func (s *Store) NewSession(ctx context.Context, userID int64) (string, error) {
	token := auth.Token()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Keep at most ten sessions per account, including the one issued below.
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND token_hash NOT IN (SELECT token_hash FROM sessions WHERE user_id=? ORDER BY created_at DESC, rowid DESC LIMIT 9)`, userID, userID); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES(?,?,?,?)`, auth.Digest(token), userID, auth.Token(), time.Now().Add(30*24*time.Hour).Unix()); err != nil {
		return "", err
	}
	return token, tx.Commit()
}
func (s *Store) Session(ctx context.Context, token string) (Session, error) {
	var session Session
	u := &session.User
	v := &u.Settings
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.username,s.csrf_token,p.display_name,p.theme,p.home_label,p.latitude,p.longitude FROM sessions s JOIN users u ON u.id=s.user_id JOIN user_settings p ON p.user_id=u.id WHERE s.token_hash=? AND s.expires_at>?`, auth.Digest(token), time.Now().Unix()).Scan(&u.ID, &u.Username, &session.CSRF, &v.DisplayName, &v.Theme, &v.HomeLabel, &v.Latitude, &v.Longitude)
	if err != nil {
		return session, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT category FROM user_interests WHERE user_id=? ORDER BY category`, u.ID)
	if err != nil {
		return session, err
	}
	defer rows.Close()
	for rows.Next() {
		var category string
		if err = rows.Scan(&category); err != nil {
			return session, err
		}
		v.Interests = append(v.Interests, category)
	}
	return session, rows.Err()
}
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, auth.Digest(token))
	return err
}
func (s *Store) CleanSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at<=?`, time.Now().Unix())
	return err
}
func (s *Store) SaveSettings(ctx context.Context, userID int64, v Settings) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE user_settings SET display_name=?,theme=?,home_label=?,latitude=?,longitude=?,updated_at=unixepoch() WHERE user_id=?`, v.DisplayName, v.Theme, v.HomeLabel, v.Latitude, v.Longitude, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_interests WHERE user_id=?`, userID); err != nil {
		return err
	}
	for _, category := range v.Interests {
		if _, err = tx.ExecContext(ctx, `INSERT INTO user_interests(user_id,category) VALUES(?,?)`, userID, category); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetAccessKey(ctx context.Context, password string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT value FROM application_settings WHERE key='access_key'`).Scan(&previous)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if !auth.CheckPassword(previous, password) {
		fingerprint, hashErr := auth.HashPassword(password)
		if hashErr != nil {
			return hashErr
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO application_settings(key,value) VALUES('access_key',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fingerprint); err != nil {
			return err
		}
	}
	return tx.Commit()
}
