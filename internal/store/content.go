package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"curio/internal/content"
)

func (s *Store) SeedContent(ctx context.Context) error {
	for _, item := range content.Items {
		if err := s.PutContent(ctx, item, nil); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) PutContent(ctx context.Context, item content.Item, media *content.Media) error {
	if err := item.Validate(); err != nil {
		return err
	}
	body, err := json.Marshal(item)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if media != nil {
		if _, err = tx.ExecContext(ctx, `INSERT INTO media(key,content_type,body) VALUES(?,?,?) ON CONFLICT(key) DO NOTHING`, media.Key, media.ContentType, media.Body); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO content_items(id,kind,article_title,payload,cached_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,article_title=excluded.article_title,payload=excluded.payload,cached_at=excluded.cached_at`, item.ID, item.Kind, item.ArticleTitle, string(body), time.Now().Unix()); err != nil {
		return err
	}
	// Replaced images can be discarded; every active article retains its own photo.
	if _, err = tx.ExecContext(ctx, `DELETE FROM media WHERE key NOT IN (SELECT json_extract(payload,'$.photo.key') FROM content_items WHERE json_extract(payload,'$.photo.key') IS NOT NULL)`); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Content(ctx context.Context, id string) (content.Item, error) {
	var body string
	var item content.Item
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM content_items WHERE id=?`, id).Scan(&body)
	if err != nil {
		return item, err
	}
	err = json.Unmarshal([]byte(body), &item)
	return item, err
}
func (s *Store) FreshArticle(ctx context.Context, title string, since time.Time) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM content_items WHERE article_title=? AND cached_at>?)`, title, since.Unix()).Scan(&exists)
	return exists, err
}
func (s *Store) Media(ctx context.Context, key string) (content.Media, error) {
	m := content.Media{Key: key}
	err := s.db.QueryRowContext(ctx, `SELECT content_type,body FROM media WHERE key=?`, key).Scan(&m.ContentType, &m.Body)
	return m, err
}
func (s *Store) Discover(ctx context.Context, r content.Request) (content.Item, error) {
	if r.Kind != "place" && r.Kind != "fact" && r.Kind != "surprise" {
		return content.Item{}, fmt.Errorf("invalid kind")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,json_extract(c.payload,'$.category'),COALESCE(h.last_seen,0) FROM content_items c LEFT JOIN user_history h ON h.content_id=c.id AND h.user_id=? AND h.last_seen>? WHERE (c.expires_at=0 OR c.expires_at>?) AND (?='surprise' OR c.kind=?) AND c.id<>? AND (?='' OR json_extract(c.payload,'$.category')=?) ORDER BY COALESCE(h.last_seen,0),c.id`, r.UserID, time.Now().Add(-content.Retention).UnixNano(), time.Now().Unix(), r.Kind, r.Kind, r.ExcludeID, r.Category, r.Category)
	if err != nil {
		return content.Item{}, err
	}
	var all []discoveryCandidate
	for rows.Next() {
		var c discoveryCandidate
		if err = rows.Scan(&c.id, &c.category, &c.seen); err != nil {
			rows.Close()
			return content.Item{}, err
		}
		all = append(all, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return content.Item{}, err
	}
	if len(all) == 0 && r.ExcludeID != "" {
		r.ExcludeID = ""
		return s.Discover(ctx, r)
	}
	if len(all) == 0 {
		return content.Item{}, sql.ErrNoRows
	}
	var unseen []discoveryCandidate
	for _, c := range all {
		if c.seen == 0 && !slices.Contains(r.RecentIDs, c.id) {
			unseen = append(unseen, c)
		}
	}
	if len(unseen) > 0 || r.UserID != 0 {
		pool := unseen
		if len(pool) == 0 {
			pool = all[:max(1, (len(all)+1)/2)]
		}
		var preferences []Preference
		if r.UserID != 0 {
			preferences, err = s.Preferences(ctx, r.UserID)
			if err != nil {
				return content.Item{}, err
			}
		}
		id, err := chooseDiscovery(pool, preferences, randomBelow)
		if err != nil {
			return content.Item{}, err
		}
		return s.Content(ctx, id)
	}
	// Guests keep a bounded oldest-first cookie; restart with its oldest eligible ID.
	for _, id := range r.RecentIDs {
		for _, c := range all {
			if id == c.id {
				return s.Content(ctx, id)
			}
		}
	}
	return s.Content(ctx, all[0].id)
}
func (s *Store) MarkSeen(ctx context.Context, userID int64, id string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_history(user_id,content_id,last_seen) VALUES(?,?,?) ON CONFLICT(user_id,content_id) DO UPDATE SET last_seen=excluded.last_seen,views=user_history.views+1`, userID, id, time.Now().UnixNano())
	return err
}
func (s *Store) ContentCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_items`).Scan(&count)
	return count, err
}
