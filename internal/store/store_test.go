package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"curio/internal/auth"
)

func TestPersistentProfilesSessionsAndRotation(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "nested")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err = s.SetAccessKey(ctx, "old-household-password"); err != nil {
		t.Fatal(err)
	}
	alice, err := s.EnterUser(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.EnterUser(ctx, "aLiCe")
	if err != nil || again != alice {
		t.Fatal("case-insensitive identity failed", err)
	}
	bob, err := s.EnterUser(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSettings(ctx, alice, Settings{DisplayName: "Alice’s corner", Theme: "dark", Interests: []string{"Space"}, Latitude: sql.NullFloat64{Float64: 52.52, Valid: true}, Longitude: sql.NullFloat64{Float64: 13.4, Valid: true}}); err != nil {
		t.Fatal(err)
	}
	aToken, err := s.NewSession(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	bToken, err := s.NewSession(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = s.db.QueryRow(`SELECT token_hash FROM sessions WHERE user_id=?`, alice).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == aToken || stored != auth.Digest(aToken) {
		t.Fatal("raw token stored")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Session(ctx, aToken)
	if err != nil || a.User.Settings.Theme != "dark" || len(a.User.Settings.Interests) != 1 {
		t.Fatal("profile did not persist", err)
	}
	b, err := s.Session(ctx, bToken)
	if err != nil || b.User.Settings.Theme != "dark" || len(b.User.Settings.Interests) != 0 {
		t.Fatal("settings leaked across users", err)
	}
	if err = s.SetAccessKey(ctx, "old-household-password"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, aToken); err != nil {
		t.Fatal("same password revoked session", err)
	}
	if err = s.SetAccessKey(ctx, "new-household-password"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, aToken); err != sql.ErrNoRows {
		t.Fatal("password rotation did not revoke sessions", err)
	}
	expired, err := s.NewSession(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE sessions SET expires_at=? WHERE token_hash=?`, time.Now().Add(-time.Second).Unix(), auth.Digest(expired)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, expired); err != sql.ErrNoRows {
		t.Fatal("expired session accepted", err)
	}
	if err = s.CleanSessions(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`INSERT INTO schema_migrations(version) VALUES(99)`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	newer, err := Open(dir)
	if err == nil {
		newer.Close()
		t.Fatal("newer schema accepted")
	}
}
func TestSettingsTransactionRollback(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	id, err := s.EnterUser(ctx, "TestUser")
	if err != nil {
		t.Fatal(err)
	}
	err = s.SaveSettings(ctx, id, Settings{DisplayName: "should roll back", Theme: "dark", Interests: []string{"Space", "Space"}})
	if err == nil {
		t.Fatal("duplicate categories should violate unique constraint")
	}
	token, err := s.NewSession(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Session(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if session.User.Settings.DisplayName != "TestUser" {
		t.Fatal("partial settings update escaped transaction")
	}
}
