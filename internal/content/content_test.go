package content

import (
	"context"
	"net/url"
	"testing"
)

func TestStarterSelection(t *testing.T) {
	for _, kind := range []string{"place", "fact", "surprise"} {
		previous := ""
		for i := 0; i < 25; i++ {
			item, err := (Starter{}).Discover(context.Background(), Request{Kind: kind, ExcludeID: previous})
			if err != nil {
				t.Fatal(err)
			}
			if item.ID == previous {
				t.Fatal("repeated immediate previous card")
			}
			if kind != "surprise" && item.Kind != kind {
				t.Fatal("wrong content kind")
			}
			previous = item.ID
		}
	}
	for _, item := range Items {
		u, err := url.Parse(item.SourceURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || item.SourceName == "" || item.ReviewedAt == "" {
			t.Fatalf("missing provenance: %s", item.ID)
		}
	}
	if _, err := (Starter{}).Discover(context.Background(), Request{Kind: "unknown"}); err == nil {
		t.Fatal("accepted unknown kind")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Starter{}).Discover(ctx, Request{Kind: "place"}); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestQuickFactsHaveValidDistinctSourcedRounds(t *testing.T) {
	seen := map[string]bool{}
	formats := map[string]int{}
	for _, item := range Items {
		if seen[item.ID] {
			t.Fatal("duplicate id", item.ID)
		}
		seen[item.ID] = true
		q := item.Quiz()
		if q.Question == "" || q.Answer < 0 || q.Answer >= len(q.Options) || q.Explanation == "" {
			t.Fatal("unplayable item", item.ID)
		}
		formats[q.Format]++
		if item.Round == nil {
			continue
		}
		if item.Photo.URL != "" || len(item.Sources) == 0 || len(item.Facts) == 0 || item.ReviewedAt == "" {
			t.Fatal("invalid quick fact provenance", item.ID)
		}
		if err := item.Validate(); err != nil {
			t.Fatal(item.ID, err)
		}
		broken := item
		copyQuiz := *item.Round
		broken.Round = &copyQuiz
		broken.Round.Answer = 2
		if broken.Validate() == nil {
			t.Fatal("out of range answer accepted")
		}
	}
	if len(Items) != 50 || formats["true-false"] < 10 || formats["comparison"] < 10 {
		t.Fatal("missing variety", formats)
	}
}
