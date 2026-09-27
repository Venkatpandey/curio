package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"slices"
	"time"

	"curio/internal/content"
)

type Preference struct {
	Category        string
	Weight          int
	Interest        bool
	Likes, Dislikes int
}

func (p Preference) Label() string {
	if p.Weight > 100 {
		return "More often"
	}
	if p.Weight < 100 {
		return "Less often"
	}
	return "Open to it"
}
func (s *Store) Preferences(ctx context.Context, uid int64) ([]Preference, error) {
	rows, err := s.db.QueryContext(ctx, `WITH categories AS (SELECT DISTINCT json_extract(payload,'$.category') AS category FROM content_items UNION SELECT category FROM user_interests WHERE user_id=?) SELECT category,EXISTS(SELECT 1 FROM user_interests i WHERE i.user_id=? AND i.category=categories.category),COALESCE((SELECT SUM(r.value='interesting') FROM reactions r WHERE r.user_id=? AND r.category=categories.category AND r.updated_at>? AND r.updated_at>COALESCE((SELECT reset_at FROM recommendation_resets WHERE user_id=?),-1)),0),COALESCE((SELECT SUM(r.value='not-for-me') FROM reactions r WHERE r.user_id=? AND r.category=categories.category AND r.updated_at>? AND r.updated_at>COALESCE((SELECT reset_at FROM recommendation_resets WHERE user_id=?),-1)),0) FROM categories ORDER BY category`, uid, uid, uid, time.Now().Add(-content.Retention).UnixNano(), uid, uid, time.Now().Add(-content.Retention).UnixNano(), uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Preference
	for rows.Next() {
		var p Preference
		if err = rows.Scan(&p.Category, &p.Interest, &p.Likes, &p.Dislikes); err != nil {
			return nil, err
		}
		p.Weight = 100 + 25*(p.Likes-p.Dislikes)
		if p.Interest {
			p.Weight += 50
		}
		p.Weight = max(25, min(300, p.Weight))
		result = append(result, p)
	}
	return result, rows.Err()
}

// Reset ignores existing reactions without erasing them or the user's collection.
func (s *Store) ResetPreferences(ctx context.Context, uid int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_interests WHERE user_id=?`, uid); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO recommendation_resets(user_id,reset_at) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET reset_at=excluded.reset_at`, uid, time.Now().UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}

type discoveryCandidate struct {
	id, category string
	seen         int64
}

func randomBelow(n int) (int, error) {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}

// Sample categories before stories so a large place catalogue cannot drown out
// smaller fact categories. One in four draws explores below the strongest weight.
func chooseDiscovery(pool []discoveryCandidate, preferences []Preference, draw func(int) (int, error)) (string, error) {
	if len(pool) == 0 {
		return "", fmt.Errorf("empty discovery pool")
	}
	weights := map[string]int{}
	for _, p := range preferences {
		weights[p.Category] = p.Weight
	}
	groups := map[string][]string{}
	for _, c := range pool {
		groups[c.category] = append(groups[c.category], c.id)
	}
	categories := make([]string, 0, len(groups))
	highest := 0
	for category := range groups {
		categories = append(categories, category)
		if weights[category] == 0 {
			weights[category] = 100
		}
		highest = max(highest, weights[category])
	}
	slices.Sort(categories)
	mode, err := draw(4)
	if err != nil {
		return "", err
	}
	if mode == 0 {
		var explore []string
		for _, category := range categories {
			if weights[category] < highest {
				explore = append(explore, category)
			}
		}
		if len(explore) > 0 {
			categories = explore
		}
		for _, category := range categories {
			weights[category] = 1
		}
	}
	total := 0
	for _, category := range categories {
		total += weights[category]
	}
	ticket, err := draw(total)
	if err != nil {
		return "", err
	}
	category := categories[0]
	for _, name := range categories {
		if ticket < weights[name] {
			category = name
			break
		}
		ticket -= weights[name]
	}
	index, err := draw(len(groups[category]))
	if err != nil {
		return "", err
	}
	return groups[category][index], nil
}
