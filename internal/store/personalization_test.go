package store

import (
	"context"
	"curio/internal/content"
	"fmt"
	"testing"
	"time"
)

func TestCollectionPaginationIsolationAndPersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	a, err := s.EnterUser(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnterUser(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 14; n++ {
		item := content.Items[0]
		item.ID = fmt.Sprintf("saved-%02d", n)
		if err = s.PutContent(ctx, item, nil); err != nil {
			t.Fatal(err)
		}
		if err = s.SaveFavorite(ctx, a, item.ID, true); err != nil {
			t.Fatal(err)
		}
		if err = s.MarkSeen(ctx, a, item.ID); err != nil {
			t.Fatal(err)
		}
	}
	rows, more, err := s.Collection(ctx, a, "favorites", 1)
	if err != nil || !more || len(rows) != 12 || rows[0].Item.ID != "saved-13" {
		t.Fatal("first page", len(rows), more, err)
	}
	second, more, err := s.Collection(ctx, a, "favorites", 2)
	if err != nil || more || len(second) != 2 || second[0].Item.ID == rows[0].Item.ID {
		t.Fatal("second page", len(second), more, err)
	}
	if err = s.SaveFavorite(ctx, a, "saved-00", true); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = s.Collection(ctx, a, "favorites", 1)
	if rows[0].Item.ID != "saved-13" {
		t.Fatal("retry reordered collection")
	}
	rows, _, err = s.Collection(ctx, b, "favorites", 1)
	if err != nil || len(rows) != 0 {
		t.Fatal("favorites leaked")
	}
	rows, _, err = s.Collection(ctx, b, "history", 1)
	if err != nil || len(rows) != 0 {
		t.Fatal("history leaked")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rows, more, err = s.Collection(ctx, a, "history", 1)
	if err != nil || !more || len(rows) != 12 || !rows[0].Favorite {
		t.Fatal("history not persisted", err)
	}
	if err = s.SaveFavorite(ctx, b, "saved-13", false); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Favorite(ctx, a, "saved-13")
	if err != nil || !saved {
		t.Fatal("other user removed favorite")
	}
	if err = s.SaveFavorite(ctx, a, "saved-13", false); err != nil {
		t.Fatal(err)
	}
	saved, err = s.Favorite(ctx, a, "saved-13")
	if err != nil || saved {
		t.Fatal("remove failed")
	}
	if _, _, err = s.Collection(ctx, a, "history", 0); err == nil {
		t.Fatal("invalid pagination")
	}
}
func TestPreferenceResetRetainsCollectionAndScores(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SeedContent(ctx); err != nil {
		t.Fatal(err)
	}
	uid, err := s.EnterUser(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO user_interests(user_id,category) VALUES(?,'Space')`, uid)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.React(ctx, uid, "venus", "interesting"); err != nil {
		t.Fatal(err)
	}
	lookup := func() Preference {
		ps, e := s.Preferences(ctx, uid)
		if e != nil {
			t.Fatal(e)
		}
		for _, p := range ps {
			if p.Category == "Space" {
				return p
			}
		}
		t.Fatal("missing category")
		return Preference{}
	}
	if p := lookup(); p.Weight != 175 || p.Likes != 1 {
		t.Fatal("interest and reaction not combined", p)
	}
	if err = s.React(ctx, uid, "venus", "not-for-me"); err != nil {
		t.Fatal(err)
	}
	if p := lookup(); p.Weight != 125 || p.Likes != 0 || p.Dislikes != 1 {
		t.Fatal("changed reaction counted twice", p)
	}
	if err = s.SaveFavorite(ctx, uid, "venus", true); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(ctx, uid, "venus", 0, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = s.MarkSeen(ctx, uid, "venus"); err != nil {
		t.Fatal(err)
	}
	if err = s.ResetPreferences(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if p := lookup(); p.Weight != 100 || p.Interest || p.Dislikes != 0 {
		t.Fatal("reset failed", p)
	}
	if r, e := s.Reaction(ctx, uid, "venus"); e != nil || r != "not-for-me" {
		t.Fatal("reset erased reaction")
	}
	if f, e := s.Favorite(ctx, uid, "venus"); e != nil || !f {
		t.Fatal("reset erased favorite")
	}
	if p, e := s.Progress(ctx, uid, time.Now()); e != nil || p.Points != 3 {
		t.Fatal("reset erased score")
	}
	if rows, _, e := s.Collection(ctx, uid, "history", 1); e != nil || len(rows) != 1 {
		t.Fatal("reset erased history")
	}
	if err = s.React(ctx, uid, "venus", "interesting"); err != nil {
		t.Fatal(err)
	}
	if p := lookup(); p.Weight != 125 {
		t.Fatal("new reaction ignored after reset", p)
	}
}
func TestRecommendationExplorationAndWeights(t *testing.T) {
	pool := []discoveryCandidate{{id: "animal", category: "Animals"}, {id: "space", category: "Space"}, {id: "space2", category: "Space"}}
	preferences := []Preference{{Category: "Animals", Weight: 25}, {Category: "Space", Weight: 300}}
	scripted := func(values ...int) func(int) (int, error) {
		n := 0
		return func(limit int) (int, error) {
			if n >= len(values) || values[n] >= limit {
				t.Fatalf("draw mismatch %v at %d limit %d", values, n, limit)
			}
			v := values[n]
			n++
			return v, nil
		}
	}
	id, err := chooseDiscovery(pool, preferences, scripted(0, 0, 0))
	if err != nil || id != "animal" {
		t.Fatal("exploration failed", id, err)
	}
	id, err = chooseDiscovery(pool, preferences, scripted(1, 25, 1))
	if err != nil || id != "space2" {
		t.Fatal("weighted selection failed", id, err)
	}
	id, err = chooseDiscovery(pool, preferences, scripted(3, 24, 0))
	if err != nil || id != "animal" {
		t.Fatal("low weight became exclusion", id, err)
	}
	id, err = chooseDiscovery(pool, nil, scripted(0, 1, 0))
	if err != nil || id != "space" {
		t.Fatal("equal-weight exploration", id, err)
	}
}
