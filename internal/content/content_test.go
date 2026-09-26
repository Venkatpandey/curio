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
