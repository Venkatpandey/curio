package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"curio/internal/content"
)

func editionIDs(e Edition) []string {
	var ids []string
	for _, i := range e.Items {
		ids = append(ids, i.ID)
	}
	return ids
}
func TestEditionPersistsAndRotatesAtLocalMidnight(t *testing.T) {
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
	uid, err := s.EnterUser(ctx, "Edition")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 23, 59, 0, 0, time.FixedZone("Home", 7200))
	first, err := s.DailyEdition(ctx, uid, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 3 || first.Items[0].Kind != "place" || first.Items[1].Category == first.Items[2].Category {
		t.Fatalf("unbalanced edition: %+v", editionIDs(first))
	}
	for _, item := range first.Items {
		if err = s.MarkSeen(ctx, uid, item.ID); err != nil {
			t.Fatal(err)
		}
		if err = s.Complete(ctx, uid, item.ID, 0, false, now); err != nil {
			t.Fatal(err)
		}
	}
	// Updating/reseeding content must not shuffle an edition.
	if err = s.SeedContent(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.DailyEdition(ctx, uid, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(editionIDs(first), editionIDs(again)) {
		t.Fatal("picks changed within day")
	}
	for _, i := range again.Items {
		if !again.Completed[i.ID] {
			t.Fatal("lost completion marker")
		}
	}
	next, err := s.DailyEdition(ctx, uid, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if next.Day != "2026-09-28" {
		t.Fatal("ignored local day", next.Day)
	}
	for _, i := range next.Items {
		if again.Completed[i.ID] {
			t.Fatal("repeated completed story while unseen stories remain", i.ID)
		}
	}
	guest, err := s.DailyEdition(ctx, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, done := range guest.Completed {
		if done {
			t.Fatal("profile progress leaked to guest")
		}
	}
}
func TestCatalogueIgnoresRefreshButDetectsNewStories(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now()
	if err = s.SeedContent(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := s.Catalogue(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SeedContent(ctx); err != nil {
		t.Fatal(err)
	}
	same, err := s.Catalogue(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision != same.Revision || before.Count != 50 {
		t.Fatal("restart counted as fresh", before.Count)
	}
	item := content.Items[0]
	item.ID = "newly-arrived"
	if err = s.PutContent(ctx, item, nil); err != nil {
		t.Fatal(err)
	}
	after, err := s.Catalogue(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if after.Count != 51 || after.Revision == before.Revision {
		t.Fatal("arrival not detected")
	}
	if !after.HasCategory("Technology") || after.HasCategory("technology") || after.HasCategory("missing") {
		t.Fatal("category contract")
	}
}
func TestEditionWorksWithSmallAndExhaustedCatalogue(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now()
	uid, err := s.EnterUser(ctx, "Small")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range content.Items[:2] {
		if err = s.PutContent(ctx, item, nil); err != nil {
			t.Fatal(err)
		}
		if err = s.Complete(ctx, uid, item.ID, 0, false, now); err != nil {
			t.Fatal(err)
		}
	}
	e, err := s.DailyEdition(ctx, uid, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Items) != 2 || e.Items[0].ID == e.Items[1].ID {
		t.Fatal("small catalogue duplicates")
	}
}
