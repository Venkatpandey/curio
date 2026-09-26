package store

import (
	"context"
	"curio/internal/content"
	"encoding/json"
	"fmt"
	"time"
)

const CollectionPageSize = 12

type CollectionEntry struct {
	Item     content.Item
	Date     string
	Favorite bool
}

func (s *Store) Favorite(ctx context.Context, uid int64, id string) (bool, error) {
	var saved bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM favorites WHERE user_id=? AND content_id=?)`, uid, id).Scan(&saved)
	return saved, err
}
func (s *Store) SaveFavorite(ctx context.Context, uid int64, id string, save bool) error {
	if !save {
		_, err := s.db.ExecContext(ctx, `DELETE FROM favorites WHERE user_id=? AND content_id=?`, uid, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO favorites(user_id,content_id,saved_at) VALUES(?,?,?) ON CONFLICT(user_id,content_id) DO NOTHING`, uid, id, time.Now().UnixNano())
	return err
}
func (s *Store) Collection(ctx context.Context, uid int64, tab string, page int) ([]CollectionEntry, bool, error) {
	if page < 1 || page > 100000 {
		return nil, false, fmt.Errorf("invalid page")
	}
	query := `SELECT c.payload,h.last_seen,EXISTS(SELECT 1 FROM favorites f WHERE f.user_id=h.user_id AND f.content_id=h.content_id) FROM user_history h JOIN content_items c ON c.id=h.content_id WHERE h.user_id=? ORDER BY h.last_seen DESC,c.id LIMIT ? OFFSET ?`
	if tab == "favorites" {
		query = `SELECT c.payload,f.saved_at,1 FROM favorites f JOIN content_items c ON c.id=f.content_id WHERE f.user_id=? ORDER BY f.saved_at DESC,c.id LIMIT ? OFFSET ?`
	} else if tab != "history" {
		return nil, false, fmt.Errorf("invalid collection")
	}
	rows, err := s.db.QueryContext(ctx, query, uid, CollectionPageSize+1, (page-1)*CollectionPageSize)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var entries []CollectionEntry
	for rows.Next() {
		var e CollectionEntry
		var body string
		var stamp int64
		if err = rows.Scan(&body, &stamp, &e.Favorite); err != nil {
			return nil, false, err
		}
		if err = json.Unmarshal([]byte(body), &e.Item); err != nil {
			return nil, false, err
		}
		e.Date = time.Unix(0, stamp).Local().Format("2 Jan 2006")
		entries = append(entries, e)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(entries) > CollectionPageSize
	if more {
		entries = entries[:CollectionPageSize]
	}
	return entries, more, nil
}
