package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"curio/internal/content"
)

type Edition struct {
	Day       string
	Items     []content.Item
	Completed map[string]bool
}

type Catalogue struct {
	Revision   string   `json:"revision"`
	Day        string   `json:"day"`
	Count      int      `json:"count"`
	Categories []string `json:"-"`
}

// Catalogue fingerprints identities, not cache timestamps. A provider refresh or
// application restart must not advertise an existing story as a new arrival.
func (s *Store) Catalogue(ctx context.Context, now time.Time) (Catalogue, error) {
	c := Catalogue{Day: now.Format("2006-01-02")}
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,json_extract(payload,'$.category') FROM content_items WHERE expires_at=0 OR expires_at>? ORDER BY id`, now.Unix())
	if err != nil {
		return c, err
	}
	defer rows.Close()
	h := sha256.New()
	categories := map[string]bool{}
	for rows.Next() {
		var id, kind, category string
		if err := rows.Scan(&id, &kind, &category); err != nil {
			return c, err
		}
		fmt.Fprintln(h, id)
		c.Count++
		if kind == "fact" {
			categories[category] = true
		}
	}
	for category := range categories {
		c.Categories = append(c.Categories, category)
	}
	sort.Strings(c.Categories)
	c.Revision = fmt.Sprintf("%x", h.Sum(nil))
	return c, rows.Err()
}

// DailyEdition pins three distinct picks on first visit. Completed stories rank
// last, unseen stories next; stable hashing rotates the remaining choices daily.
func (s *Store) DailyEdition(ctx context.Context, userID int64, now time.Time) (Edition, error) {
	edition := Edition{Day: now.Format("2006-01-02"), Completed: map[string]bool{}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return edition, err
	}
	defer tx.Rollback()
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT items FROM daily_editions WHERE profile_id=? AND day=?`, userID, edition.Day).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		var previousRaw string
		var previous []string
		previousErr := tx.QueryRowContext(ctx, `SELECT items FROM daily_editions WHERE profile_id=? AND day<? ORDER BY day DESC LIMIT 1`, userID, edition.Day).Scan(&previousRaw)
		if previousErr == nil {
			if err = json.Unmarshal([]byte(previousRaw), &previous); err != nil {
				return edition, err
			}
		} else if !errors.Is(previousErr, sql.ErrNoRows) {
			return edition, previousErr
		}

		rows, err := tx.QueryContext(ctx, `SELECT c.id,c.kind,json_extract(c.payload,'$.category'),EXISTS(SELECT 1 FROM discoveries d WHERE d.user_id=? AND d.content_id=c.id),EXISTS(SELECT 1 FROM user_history h WHERE h.user_id=? AND h.content_id=c.id AND h.last_seen>?) FROM content_items c WHERE c.expires_at=0 OR c.expires_at>? ORDER BY c.id`, userID, userID, now.Add(-content.Retention).UnixNano(), now.Unix())
		if err != nil {
			return edition, err
		}
		type candidate struct {
			id, kind, key, category   string
			completed, seen, previous bool
		}
		var pool []candidate
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.id, &c.kind, &c.category, &c.completed, &c.seen); err != nil {
				rows.Close()
				return edition, err
			}
			c.key = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", edition.Day, userID, c.id))))
			c.previous = slices.Contains(previous, c.id)
			pool = append(pool, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return edition, err
		}
		sort.Slice(pool, func(i, j int) bool {
			if pool[i].completed != pool[j].completed {
				return !pool[i].completed
			}
			if pool[i].previous != pool[j].previous {
				return !pool[i].previous
			}
			if pool[i].seen != pool[j].seen {
				return !pool[i].seen
			}
			return pool[i].key < pool[j].key
		})
		var ids []string
		selectedCategories := map[string]bool{}
		used := map[string]bool{}
		// One photographic escape, then two facts. Fall back if a kind runs out.
		for _, kind := range []string{"place", "fact", "fact"} {
			picked := false
			hasUncompleted := false
			for _, c := range pool {
				if !used[c.id] && c.kind == kind && !c.completed {
					hasUncompleted = true
					break
				}
			}
			for pass := 0; pass < 2 && !picked; pass++ {
				for _, c := range pool {
					if !used[c.id] && c.kind == kind && (!hasUncompleted || !c.completed) && (pass == 1 || !selectedCategories[c.category]) {
						ids = append(ids, c.id)
						used[c.id] = true
						selectedCategories[c.category] = true
						picked = true
						break
					}
				}
			}
		}
		for _, c := range pool {
			if len(ids) >= 3 {
				break
			}
			if !used[c.id] {
				ids = append(ids, c.id)
				used[c.id] = true
			}
		}
		body, err := json.Marshal(ids)
		if err != nil {
			return edition, err
		}
		raw = string(body)
		if _, err = tx.ExecContext(ctx, `INSERT INTO daily_editions(profile_id,day,items) VALUES(?,?,?)`, userID, edition.Day, raw); err != nil {
			return edition, err
		}
		// Keep storage bounded while preserving today's picks across restarts.
		if _, err = tx.ExecContext(ctx, `DELETE FROM daily_editions WHERE day<?`, now.AddDate(0, 0, -7).Format("2006-01-02")); err != nil {
			return edition, err
		}
	} else if err != nil {
		return edition, err
	}
	var ids []string
	if err = json.Unmarshal([]byte(raw), &ids); err != nil {
		return edition, err
	}
	for _, id := range ids {
		var body string
		if err = tx.QueryRowContext(ctx, `SELECT payload FROM content_items WHERE id=?`, id).Scan(&body); err != nil {
			return edition, err
		}
		var item content.Item
		if err = json.Unmarshal([]byte(body), &item); err != nil {
			return edition, err
		}
		edition.Items = append(edition.Items, item)
		var completed bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM discoveries WHERE user_id=? AND content_id=?)`, userID, id).Scan(&completed); err != nil {
			return edition, err
		}
		edition.Completed[id] = completed
	}
	return edition, tx.Commit()
}

func (c Catalogue) HasCategory(category string) bool {
	return slices.Contains(c.Categories, category)
}
