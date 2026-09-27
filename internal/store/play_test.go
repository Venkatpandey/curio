package store

import (
	"context"
	"curio/internal/content"
	"sync"
	"testing"
	"time"
)

func TestPlayPersistsIsolatesAndRejectsDuplicatePoints(t *testing.T) {
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
	now := time.Date(2026, 9, 26, 23, 59, 0, 0, time.FixedZone("Home", 7200))
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.Complete(ctx, alice, "venus", 0, true, now); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if err = s.Complete(ctx, alice, "octopus", 0, false, now); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(ctx, alice, "neutron", -1, false, now); err != nil {
		t.Fatal(err)
	}
	p, err := s.Progress(ctx, alice, now)
	if err != nil || p.Points != 7 || p.Today != 3 || p.Correct != 1 || p.Guesses != 2 {
		t.Fatalf("bad progress: %+v %v", p, err)
	}
	p, err = s.Progress(ctx, bob, now)
	if err != nil || p.Points != 0 || p.Today != 0 {
		t.Fatal("profile leak", p, err)
	}
	if err = s.React(ctx, alice, "venus", "interesting"); err != nil {
		t.Fatal(err)
	}
	if err = s.React(ctx, alice, "venus", "not-for-me"); err != nil {
		t.Fatal(err)
	}
	if err = s.React(ctx, alice, "venus", "invalid"); err == nil {
		t.Fatal("invalid reaction saved")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Attempt(ctx, alice, "venus")
	if err != nil || a == nil || a.Points != 3 || !a.Correct {
		t.Fatal("lost attempt", a, err)
	}
	value, err := s.Reaction(ctx, alice, "venus")
	if err != nil || value != "not-for-me" {
		t.Fatal(value, err)
	}
	value, err = s.Reaction(ctx, bob, "venus")
	if err != nil || value != "" {
		t.Fatal("reaction leaked", err)
	}
	if err = s.React(ctx, alice, "venus", "clear"); err != nil {
		t.Fatal(err)
	}
	value, err = s.Reaction(ctx, alice, "venus")
	if err != nil || value != "" {
		t.Fatal("reaction not cleared")
	}
	tomorrow := now.Add(2 * time.Minute)
	p, err = s.Progress(ctx, alice, tomorrow)
	if err != nil || p.Today != 0 || p.Points != 7 {
		t.Fatal("daily reset lost points", p, err)
	}
	if err = s.Complete(ctx, alice, "venus", 0, true, tomorrow); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Progress(ctx, alice, tomorrow)
	if p.Today != 0 || p.Points != 7 {
		t.Fatal("replay earned points", p)
	}
	item, err := s.Discover(ctx, content.Request{Kind: "fact", Category: "Animals", ExcludeID: "octopus"})
	if err != nil || item.Category != "Animals" || item.ID == "octopus" {
		t.Fatal("category filter or exclusion failed", err)
	}
	single, err := s.Content(ctx, "octopus")
	if err != nil {
		t.Fatal(err)
	}
	single.Category = "Single item fixture"
	if err = s.PutContent(ctx, single, nil); err != nil {
		t.Fatal(err)
	}
	only, err := s.Discover(ctx, content.Request{Kind: "fact", Category: single.Category, ExcludeID: single.ID})
	if err != nil || only.ID != single.ID {
		t.Fatal("single-item fallback unavailable", err)
	}
}
