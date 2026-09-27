package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"curio/internal/content"
)

type Progress struct{ Points, Discoveries, Today, Guesses, Correct int }

func (p Progress) Daily() int { return min(p.Today, 3) }
func (p Progress) Badges() []string {
	var badges []string
	if p.Discoveries >= 1 {
		badges = append(badges, "First spark")
	}
	if p.Discoveries >= 10 {
		badges = append(badges, "Curiosity collector")
	}
	if p.Guesses >= 3 {
		badges = append(badges, "Game for a guess")
	}
	return badges
}

type Attempt struct {
	Answer  int
	Correct bool
	Points  int
}

func (s *Store) Attempt(ctx context.Context, uid int64, id string) (*Attempt, error) {
	var a Attempt
	err := s.db.QueryRowContext(ctx, `SELECT answer,correct,points FROM discoveries WHERE user_id=? AND content_id=?`, uid, id).Scan(&a.Answer, &a.Correct, &a.Points)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &a, err
}

// The unique key awards points once per discovery, including concurrent retries.
func (s *Store) Complete(ctx context.Context, uid int64, id string, answer int, correct bool, now time.Time) error {
	points := 1
	if answer >= 0 {
		points = 3
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO discoveries(user_id,content_id,day,answer,correct,points) SELECT ?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM content_items WHERE id=?) ON CONFLICT(user_id,content_id) DO NOTHING`, uid, id, now.Format("2006-01-02"), answer, correct, points, id)
	return err
}
func (s *Store) Progress(ctx context.Context, uid int64, now time.Time) (Progress, error) {
	var p Progress
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(points),0),COUNT(*),COALESCE(SUM(day=?),0),COALESCE(SUM(answer>=0),0),COALESCE(SUM(correct),0) FROM discoveries WHERE user_id=?`, now.Format("2006-01-02"), uid).Scan(&p.Points, &p.Discoveries, &p.Today, &p.Guesses, &p.Correct)
	return p, err
}
func (s *Store) React(ctx context.Context, uid int64, id, value string) error {
	if value != "interesting" && value != "not-for-me" && value != "clear" {
		return fmt.Errorf("invalid reaction")
	}
	if value == "clear" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM reactions WHERE user_id=? AND content_id=?`, uid, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO reactions(user_id,content_id,value,updated_at,category) SELECT ?,?,?,?,json_extract(payload,'$.category') FROM content_items WHERE id=? ON CONFLICT(user_id,content_id) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at,category=excluded.category`, uid, id, value, time.Now().UnixNano(), id)
	return err
}
func (s *Store) Reaction(ctx context.Context, uid int64, id string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM reactions WHERE user_id=? AND content_id=? AND updated_at>?`, uid, id, time.Now().Add(-content.Retention).UnixNano()).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}
