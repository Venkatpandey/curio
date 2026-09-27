package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"curio/internal/content"
)

func TestFeedMigrationPreservesExistingUserData(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "curio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for n, file := range files[:5] {
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, n+1); err != nil {
			t.Fatal(err)
		}
	}
	i := content.Items[0]
	body, _ := json.Marshal(i)
	if _, err = db.Exec(`INSERT INTO users(id,username) VALUES(1,'Existing')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO content_items(id,kind,payload,cached_at) VALUES(?,?,?,?)`, i.ID, i.Kind, string(body), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO discoveries VALUES(1,?,'2026-09-26',1,1,3)`, i.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO reactions(user_id,content_id,value,updated_at) VALUES(1,?,'interesting',?)`, i.ID, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO favorites VALUES(1,?,?)`, i.ID, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	p, err := s.Progress(ctx, 1, time.Now())
	if err != nil || p.Points != 3 || p.Correct != 1 {
		t.Fatal("migration lost awards", p, err)
	}
	saved, err := s.Favorite(ctx, 1, i.ID)
	if err != nil || !saved {
		t.Fatal("migration lost favorite", err)
	}
	prefs, err := s.Preferences(ctx, 1)
	if err != nil || len(prefs) != 1 || prefs[0].Likes != 1 {
		t.Fatal("migration lost reaction", prefs, err)
	}
	var violations int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatal("foreign key violation", err)
	}
	if _, err = s.db.Exec(`DELETE FROM users WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM discoveries)+(SELECT COUNT(*) FROM reactions)+(SELECT COUNT(*) FROM favorites)`).Scan(&violations); err != nil || violations != 0 {
		t.Fatal("user deletion left private data", err)
	}
}

func feedItem(name string, published time.Time) content.Item {
	u := "https://science.nasa.gov/" + name + "/"
	return content.Item{ID: fmt.Sprintf("nasa-%x", sha256.Sum256([]byte(u))), Kind: "fact", Title: name, Category: "Science", Summary: "A source excerpt about observations made by researchers studying our changing planet and its oceans.", SourceName: "NASA", SourceURL: u, FetchedAt: published, Feed: &content.FeedInfo{Provider: "nasa-science", PublishedAt: published, Validation: "source-excerpt-v1"}, Sections: []content.Section{{Heading: "From NASA", Text: "Source wording."}}, Sources: []content.Source{{Name: "NASA", URL: u}}}
}

func TestFeedImportExpiryFavoritesAndAwards(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC().Truncate(time.Second)
	if err = s.SeedContent(ctx); err != nil {
		t.Fatal(err)
	}
	uid, err := s.EnterUser(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	items := []content.Item{feedItem("expired", now.Add(-content.Retention+time.Hour)), feedItem("saved", now.Add(-content.Retention+time.Hour)), feedItem("pinned", now.Add(-content.Retention+time.Hour)), feedItem("recent", now)}
	if err = s.ImportFeed(ctx, "nasa-science", items, now); err != nil {
		t.Fatal(err)
	}
	if err = s.ImportFeed(ctx, "nasa-science", items, now); err != nil {
		t.Fatal(err)
	}
	count, _ := s.ContentCount(ctx)
	if count != len(content.Items)+4 {
		t.Fatal("duplicate import", count)
	}
	if err = s.SaveFavorite(ctx, uid, items[1].ID, true); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(ctx, uid, items[0].ID, -1, false, now); err != nil {
		t.Fatal(err)
	}
	if err = s.React(ctx, uid, items[0].ID, "interesting"); err != nil {
		t.Fatal(err)
	}
	if err = s.MarkSeen(ctx, uid, items[0].ID); err != nil {
		t.Fatal(err)
	}
	// Cleanup at expiry itself must remove the unsaved item, but not today's pick.
	later := now.Add(time.Hour)
	raw, _ := json.Marshal([]string{items[2].ID})
	if _, err = s.db.Exec(`INSERT INTO daily_editions(profile_id,day,items) VALUES(?,?,?)`, uid, later.Format("2006-01-02"), string(raw)); err != nil {
		t.Fatal(err)
	}
	if err = s.CleanupContent(ctx, later); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Content(ctx, items[0].ID); err != sql.ErrNoRows {
		t.Fatal("expired payload retained", err)
	}
	for _, i := range items[1:] {
		if _, err = s.Content(ctx, i.ID); err != nil {
			t.Fatal("protected payload removed", i.ID, err)
		}
	}
	p, err := s.Progress(ctx, uid, later)
	if err != nil || p.Points != 1 {
		t.Fatal("expiry erased points", p, err)
	}
	// Reaction learning survives payload expiry for the remainder of its window.
	prefs, err := s.Preferences(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range prefs {
		if p.Category == "Science" && p.Likes != 1 {
			t.Fatal("recent signal lost", p)
		}
	}
	latest, err := s.LatestUpdates(ctx, later)
	if err != nil || len(latest) != 1 || latest[0].ID != items[3].ID {
		t.Fatal("expired saved item recommended", latest, err)
	}
	catalogue, err := s.Catalogue(ctx, later)
	if err != nil || catalogue.Count != len(content.Items)+1 {
		t.Fatal("catalogue includes expired items", catalogue, err)
	}
	// Even if a source later republishes the URL, its existing award stays unique.
	republished := items[0]
	republished.Feed = &content.FeedInfo{Provider: "nasa-science", PublishedAt: later, Validation: "source-excerpt-v1"}
	republished.FetchedAt = later
	if err = s.ImportFeed(ctx, "nasa-science", []content.Item{republished}, later); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(ctx, uid, republished.ID, -1, false, later); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Progress(ctx, uid, later)
	if p.Points != 1 {
		t.Fatal("repeat award after expiry")
	}
	if err = s.CleanupContent(ctx, later.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Content(ctx, items[2].ID); err != sql.ErrNoRows {
		t.Fatal("yesterday's pinned payload retained")
	}
}

func TestSevenDaySignalsAndImportAtomicity(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SeedContent(ctx); err != nil {
		t.Fatal(err)
	}
	uid, _ := s.EnterUser(ctx, "Alice")
	bob, _ := s.EnterUser(ctx, "Bob")
	now := time.Now().UTC()
	if err = s.SaveSettings(ctx, uid, Settings{DisplayName: "Alice", Theme: "dark", Interests: []string{"Space"}}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []int64{uid, bob} {
		if err = s.React(ctx, u, "venus", "interesting"); err != nil {
			t.Fatal(err)
		}
		if err = s.MarkSeen(ctx, u, "venus"); err != nil {
			t.Fatal(err)
		}
	}
	cutoff := now.Add(-content.Retention).UnixNano()
	if _, err = s.db.Exec(`UPDATE reactions SET updated_at=? WHERE user_id=?`, cutoff, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE user_history SET last_seen=? WHERE user_id=?`, cutoff, uid); err != nil {
		t.Fatal(err)
	}
	prefs, err := s.Preferences(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range prefs {
		if p.Category == "Space" && (p.Weight != 150 || p.Likes != 0) {
			t.Fatal("old reaction affects ranking", p)
		}
	}
	rows, _, err := s.Collection(ctx, uid, "history", 1)
	if err != nil || len(rows) != 0 {
		t.Fatal("old history visible before cleanup")
	}
	if err = s.CleanupContent(ctx, now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM reactions WHERE user_id=?`, uid).Scan(&count); err != nil || count != 0 {
		t.Fatal("old reactions retained")
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM reactions WHERE user_id=?`, bob).Scan(&count); err != nil || count != 1 {
		t.Fatal("other profile signal lost")
	}
	valid := feedItem("valid", now)
	bad := feedItem("invalid", now)
	bad.SourceURL = "https://evil.example/"
	if s.ImportFeed(ctx, "nasa-science", []content.Item{valid, bad}, now) == nil {
		t.Fatal("invalid batch accepted")
	}
	if _, err = s.Content(ctx, valid.ID); err != sql.ErrNoRows {
		t.Fatal("partial batch published")
	}
}

func TestFeedSchedulePersistsAndRefreshDoesNotExtendExpiry(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	state := content.FeedState{LastAttempt: now, NextAttempt: now.Add(4 * time.Hour), LastError: "Upstream HTTP 429", Failures: 1}
	if err = s.SaveFeedState(ctx, "nasa-science", state); err != nil {
		t.Fatal(err)
	}
	i := feedItem("stable", now.Add(-time.Hour))
	if err = s.ImportFeed(ctx, "nasa-science", []content.Item{i}, now); err != nil {
		t.Fatal(err)
	}
	originalExpiry := i.Feed.PublishedAt.Add(content.Retention).Unix()
	i.Feed.PublishedAt = now
	i.FetchedAt = now
	if err = s.ImportFeed(ctx, "nasa-science", []content.Item{i}, now); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.FeedState(ctx, "nasa-science")
	if err != nil || got != state {
		t.Fatal("schedule lost", got, err)
	}
	var expiry int64
	if err = s.db.QueryRow(`SELECT expires_at FROM content_items WHERE id=?`, i.ID).Scan(&expiry); err != nil || expiry != originalExpiry {
		t.Fatal("refresh renewed expiry", expiry, err)
	}
}
