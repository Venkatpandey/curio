package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"curio/internal/content"
)

func TestDiscoverySeenTrackingAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err = s.SeedContent(ctx); err != nil {
		t.Fatal(err)
	}
	alice, err := s.EnterUser(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.EnterUser(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	last := ""
	for n := 0; n < 3; n++ {
		item, err := s.Discover(ctx, content.Request{Kind: "place", UserID: alice, ExcludeID: last})
		if err != nil {
			t.Fatal(err)
		}
		if seen[item.ID] {
			t.Fatal("repeated before collection exhausted")
		}
		seen[item.ID] = true
		last = item.ID
		if err = s.MarkSeen(ctx, alice, item.ID); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM user_history WHERE user_id=?`, bob).Scan(&count); err != nil || count != 0 {
		t.Fatal("seen state leaked", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM user_history WHERE user_id=?`, alice).Scan(&count); err != nil || count != 3 {
		t.Fatal("seen state lost on restart", err)
	}
	item, err := s.Discover(ctx, content.Request{Kind: "place", UserID: alice, ExcludeID: last})
	if err != nil || item.ID == last {
		t.Fatal("exhaustion repeated immediate card", err)
	}
	guest, err := s.Discover(ctx, content.Request{Kind: "place", RecentIDs: []string{"tristan", "socotra"}})
	if err != nil || guest.ID != "bryce" {
		t.Fatal("guest recent list ignored", err)
	}
}
func TestCacheRetainsMediaAndSource(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	item := content.Items[0]
	item.ID = "remote-photo"
	item.Photo.Key = "cached-key"
	item.Photo.URL = "/media/cached-key"
	if err = s.PutContent(ctx, item, &content.Media{Key: "cached-key", ContentType: "image/jpeg", Body: []byte("cached fixture")}); err != nil {
		t.Fatal(err)
	}
	if err = s.SeedContent(ctx); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Content(ctx, item.ID)
	if err != nil || loaded.Photo.Artist != item.Photo.Artist {
		t.Fatal("attribution lost", err)
	}
	media, err := s.Media(ctx, "cached-key")
	if err != nil || string(media.Body) != "cached fixture" {
		t.Fatal("seed refresh destroyed cached media", err)
	}
	item.Photo.Key = "new-key"
	item.Photo.URL = "/media/new-key"
	if err = s.PutContent(ctx, item, &content.Media{Key: "new-key", ContentType: "image/jpeg", Body: []byte("replacement")}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Media(ctx, "cached-key"); err != sql.ErrNoRows {
		t.Fatal("orphan photo retained", err)
	}
}
func TestPhaseOneMigrationPreservesProfiles(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "curio.db"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("migrations/001_accounts.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)`, `INSERT INTO schema_migrations(version) VALUES(1)`, `INSERT INTO users(id,username) VALUES(1,'Existing')`, `INSERT INTO user_settings(user_id,display_name,theme) VALUES(1,'My saved name','light')`} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	token, err := s.NewSession(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Session(ctx, token)
	if err != nil || session.User.Settings.DisplayName != "My saved name" || session.User.Settings.Theme != "light" {
		t.Fatal("migration changed saved profile", err)
	}
	if err = s.SeedContent(ctx); err != nil {
		t.Fatal(err)
	}
	item, err := s.Content(ctx, "tristan")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = json.Marshal(item); err != nil {
		t.Fatal(err)
	}
}
