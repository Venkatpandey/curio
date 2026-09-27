package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"curio/internal/content"
)

func (s *Store) FeedState(ctx context.Context, provider string) (content.FeedState, error) {
	var state content.FeedState
	var body string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM feed_state WHERE provider=?`, provider).Scan(&body)
	if err == sql.ErrNoRows {
		return state, nil
	}
	if err == nil {
		err = json.Unmarshal([]byte(body), &state)
	}
	return state, err
}

func (s *Store) SaveFeedState(ctx context.Context, provider string, state content.FeedState) error {
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO feed_state(provider,payload) VALUES(?,?) ON CONFLICT(provider) DO UPDATE SET payload=excluded.payload`, provider, string(body))
	return err
}

func (s *Store) LatestUpdates(ctx context.Context, now time.Time) ([]content.Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM content_items WHERE expires_at>? ORDER BY expires_at DESC,id LIMIT 3`, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []content.Item
	for rows.Next() {
		var raw string
		var item content.Item
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ImportFeed(ctx context.Context, provider string, items []content.Item, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range items {
		if err = content.ValidateFeedItem(item, provider, now); err != nil {
			return err
		}
		body, err := json.Marshal(item)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO content_items(id,kind,payload,cached_at,expires_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,cached_at=excluded.cached_at,expires_at=MIN(content_items.expires_at,excluded.expires_at)`, item.ID, item.Kind, string(body), now.Unix(), item.Feed.PublishedAt.Add(content.Retention).Unix())
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CleanupContent runs even with providers disabled. Saved favorites and today's
// pinned picks retain their payloads; expired picks cannot enter a new edition.
func (s *Store) CleanupContent(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cutoff := now.Add(-content.Retention)
	for _, query := range []string{`DELETE FROM user_history WHERE last_seen<=?`, `DELETE FROM reactions WHERE updated_at<=?`} {
		if _, err = tx.ExecContext(ctx, query, cutoff.UnixNano()); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM daily_editions WHERE day<?`, cutoff.Format("2006-01-02")); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM content_items WHERE expires_at>0 AND expires_at<=? AND id NOT IN (SELECT content_id FROM favorites) AND id NOT IN (SELECT j.value FROM daily_editions e,json_each(e.items) j WHERE e.day=?)`, now.Unix(), now.Format("2006-01-02")); err != nil {
		return err
	}
	return tx.Commit()
}
