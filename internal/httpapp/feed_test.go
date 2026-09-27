package httpapp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"curio/internal/content"
)

func TestFeedReadingCompletionAndIsolation(t *testing.T) {
	f := setup(t, true)
	now := time.Now().UTC()
	source := "https://science.nasa.gov/universe/test-galaxy/"
	i := content.Item{ID: fmt.Sprintf("nasa-%x", sha256.Sum256([]byte(source))), Kind: "fact", Title: "A galaxy update", Summary: "Researchers studied a galaxy using observations collected over several years. <script>alert(1)</script>", Category: "Space", SourceName: "NASA", SourceURL: source, FetchedAt: now, Feed: &content.FeedInfo{Provider: "nasa-science", PublishedAt: now.Add(-time.Hour), Validation: "source-excerpt-v1"}, Sections: []content.Section{{Heading: "From NASA", Text: "Source wording."}}, Sources: []content.Source{{Name: "NASA", URL: source}}}
	if err := f.db.ImportFeed(context.Background(), "nasa-science", []content.Item{i}, now); err != nil {
		t.Fatal(err)
	}
	c := f.client()
	_, home := f.get(c, "/")
	if !strings.Contains(home, "Fresh from the source") || !strings.Contains(home, i.Title) {
		t.Fatal("new update absent from homepage")
	}
	path := "/discover?kind=fact&id=" + i.ID
	csrf := f.csrf(c, path)
	res, body := f.get(c, path)
	if res.StatusCode != 200 || !strings.Contains(body, i.Title) || !strings.Contains(body, "Published") || !strings.Contains(body, "Fetched") || !strings.Contains(body, source) || strings.Contains(body, `class="guess-options"`) || strings.Contains(body, "<script>alert") {
		t.Fatal("source update not safely readable", res.StatusCode, body)
	}
	values := url.Values{"csrf": {csrf}, "id": {i.ID}, "kind": {"fact"}, "answer": {"0"}}
	res, _ = f.post(c, "/play", values)
	if res.StatusCode != 400 {
		t.Fatal("fabricated quiz accepted")
	}
	values.Set("answer", "-1")
	res, body = f.post(c, "/play", values)
	if res.StatusCode != 200 || !strings.Contains(body, "Discovery complete.") || strings.Contains(body, "point banked") {
		t.Fatal("guest completion", res.StatusCode)
	}
	f.enter(c, "FeedReader")
	values.Set("csrf", f.csrf(c, path))
	for n := 0; n < 2; n++ {
		res, _ = f.post(c, "/play", values)
		if res.StatusCode != 303 {
			t.Fatal("completion failed")
		}
	}
	uid, _ := f.db.EnterUser(context.Background(), "FeedReader")
	p, err := f.db.Progress(context.Background(), uid, now)
	if err != nil || p.Points != 1 || p.Discoveries != 1 || p.Guesses != 0 {
		t.Fatal("incorrect read award", p, err)
	}
	b := f.client()
	f.enter(b, "OtherReader")
	_, body = f.get(b, path)
	if strings.Contains(body, "Discovery complete.") {
		t.Fatal("read completion leaked across profiles")
	}
}
