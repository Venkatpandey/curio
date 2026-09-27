package content

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func rssItem(link, title, description string, date time.Time) string {
	return fmt.Sprintf(`<item><title>%s</title><link>%s</link><pubDate>%s</pubDate><description><![CDATA[%s]]></description><category>Spiral Galaxies</category></item>`, title, link, date.Format(time.RFC1123Z), description)
}

const testExcerpt = "Astronomers studied a distant galaxy using observations collected by the telescope over several years."

func TestFeedValidationAndDeduplication(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	good := rssItem("https://science.nasa.gov/universe/galaxy/", "A galaxy", "<p>"+testExcerpt+"</p>", now.Add(-time.Hour))
	body := `<rss><channel><title>NASA Science</title>` + good + good +
		rssItem("https://evil.example/story/", "Bad host", testExcerpt, now) +
		rssItem("https://science.nasa.gov@evil.example/story/", "User info", testExcerpt, now) +
		rssItem("https://science.nasa.gov/old/", "Old", testExcerpt, now.Add(-Retention)) +
		rssItem("https://science.nasa.gov/future/", "Future", testExcerpt, now.Add(time.Second)) +
		rssItem("https://science.nasa.gov/short/", "Short", "Two words", now) +
		rssItem("https://science.nasa.gov/script/", "Script", "<script>alert(1)</script>"+testExcerpt, now) +
		rssItem("https://science.nasa.gov/apod/", "APOD: something", testExcerpt, now) + `</channel></rss>`
	items, err := parseNASAFeed([]byte(body), "nasa-science", now)
	if err != nil || len(items) != 1 {
		t.Fatalf("validated pool: %d, %v", len(items), err)
	}
	i := items[0]
	if i.Summary != testExcerpt || i.Category != "Space" || len(i.Quiz().Options) != 0 || i.ReviewedAt != "" || i.Feed.PublishedAt.Equal(i.FetchedAt) {
		t.Fatalf("source text/metadata changed: %+v", i)
	}
	for _, bad := range []string{`<html>error</html>`, `<rss><channel>`, `<rss/>`} {
		if _, err := parseNASAFeed([]byte(bad), "nasa-science", now); err == nil {
			t.Fatal("malformed response accepted")
		}
	}
	i.Round = &Quiz{Format: "true-false", Question: "Invented", Options: []string{"True", "False"}, Explanation: "Invented"}
	if ValidateFeedItem(i, "nasa-science", now) == nil {
		t.Fatal("unreviewed quiz accepted")
	}
}

type feedTransport func(*http.Request) (*http.Response, error)

func (f feedTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFeedHTTPBoundsAndRetryAfter(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name, body, retry string
		status            int
	}{
		{"oversize", strings.Repeat("x", (2<<20)+1), "", 200},
		{"malformed", "<rss", "", 200},
		{"rate limit", "", "3600", 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NASAFeeds()[0].(*NASAFeed)
			f.Client.Transport = feedTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("User-Agent") != UserAgent || r.Context() == nil {
					t.Fatal("missing request identity")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": {tc.retry}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			_, err := f.Fetch(context.Background(), now)
			if err == nil {
				t.Fatal("upstream failure accepted")
			}
			if tc.status == 429 && err.(*upstreamError).retryAfter != time.Hour {
				t.Fatal("retry-after lost")
			}
		})
	}
	f := NASAFeeds()[0].(*NASAFeed)
	for _, raw := range []string{"https://evil.example/feed", "http://science.nasa.gov/feed", "https://science.nasa.gov:8443/feed"} {
		req, _ := http.NewRequest("GET", raw, nil)
		if f.Client.CheckRedirect(req, nil) == nil {
			t.Fatal("unsafe redirect accepted")
		}
	}
}

type workerCache struct {
	state   FeedState
	imports int
}

func (c *workerCache) FeedState(context.Context, string) (FeedState, error) { return c.state, nil }
func (c *workerCache) SaveFeedState(_ context.Context, _ string, s FeedState) error {
	c.state = s
	return nil
}
func (c *workerCache) ImportFeed(context.Context, string, []Item, time.Time) error {
	c.imports++
	return nil
}

type workerSource struct {
	calls int
	err   error
}

func (s *workerSource) Name() string { return "test" }
func (s *workerSource) Fetch(context.Context, time.Time) ([]Item, error) {
	s.calls++
	return nil, s.err
}

func TestWorkerScheduleRecoveryAndCancellation(t *testing.T) {
	now := time.Now().UTC()
	cache := &workerCache{}
	source := &workerSource{err: &upstreamError{status: 429, retryAfter: 2 * time.Hour}}
	w := FeedWorker{Source: source, Cache: cache}
	if w.Tick(context.Background(), now) == nil || !cache.state.NextAttempt.Equal(now.Add(2*time.Hour)) || cache.imports != 0 {
		t.Fatal("retry not persisted")
	}
	// A newly constructed worker honors the previous process's schedule.
	w = FeedWorker{Source: source, Cache: cache}
	if err := w.Tick(context.Background(), now.Add(time.Hour)); err != nil || source.calls != 1 {
		t.Fatal("restart ignored backoff")
	}
	source.err = nil
	if err := w.Tick(context.Background(), now.Add(2*time.Hour)); err != nil || cache.imports != 1 || cache.state.Failures != 0 || cache.state.LastError != "" || !cache.state.NextAttempt.Equal(now.Add(6*time.Hour)) {
		t.Fatal("recovery failed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.Run(ctx)
	if source.calls != 2 {
		t.Fatal("cancelled worker fetched")
	}
}
