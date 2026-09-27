package content

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const Retention = 7 * 24 * time.Hour
const FeedInterval = 4 * time.Hour

// FeedInfo records automated provenance validation, not an editorial review.
type FeedInfo struct {
	Provider    string    `json:"provider"`
	PublishedAt time.Time `json:"published_at"`
	Validation  string    `json:"validation"`
}

type FeedSource interface {
	Name() string
	Fetch(context.Context, time.Time) ([]Item, error)
}

type FeedState struct {
	LastAttempt, LastSuccess, NextAttempt time.Time
	Failures, Accepted                    int
	LastError                             string
}

type FeedCache interface {
	FeedState(context.Context, string) (FeedState, error)
	SaveFeedState(context.Context, string, FeedState) error
	ImportFeed(context.Context, string, []Item, time.Time) error
}

type FeedWorker struct {
	Source FeedSource
	Cache  FeedCache
	Logger *slog.Logger
}

// Tick persists the next attempt before networking so restarts cannot hammer
// an unavailable source. A successful fetch never changes publication dates.
func (w FeedWorker) Tick(ctx context.Context, now time.Time) error {
	s, err := w.Cache.FeedState(ctx, w.Source.Name())
	if err != nil || now.Before(s.NextAttempt) {
		return err
	}
	s.LastAttempt, s.NextAttempt = now, now.Add(FeedInterval)
	if err = w.Cache.SaveFeedState(ctx, w.Source.Name(), s); err != nil {
		return err
	}
	request, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	items, err := w.Source.Fetch(request, now)
	if err == nil {
		err = w.Cache.ImportFeed(request, w.Source.Name(), items, now)
	}
	if err != nil {
		s.Failures++
		delay := time.Minute * time.Duration(1<<min(s.Failures-1, 8))
		s.LastError = "Fetch or validation failed; cached content remains available."
		var upstream *upstreamError
		if errors.As(err, &upstream) {
			delay = max(delay, min(upstream.retryAfter, 24*time.Hour))
			s.LastError = fmt.Sprintf("Upstream HTTP %d", upstream.status)
		}
		s.NextAttempt = now.Add(delay)
	} else {
		s.LastSuccess, s.Failures, s.LastError, s.Accepted = now, 0, "", len(items)
		if w.Logger != nil {
			w.Logger.Info("Source updates cached", "provider", w.Source.Name(), "accepted", len(items))
		}
	}
	if saveErr := w.Cache.SaveFeedState(ctx, w.Source.Name(), s); saveErr != nil {
		return saveErr
	}
	return err
}

func (w FeedWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := w.Tick(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			// Do not log external bodies or URLs supplied by a feed.
			w.Logger.Warn("Source update deferred", "provider", w.Source.Name())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type NASAFeed struct {
	Client   *http.Client
	Endpoint string
	ID       string
}

func NASAFeeds() []FeedSource {
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !nasaURL(r.URL) {
			return fmt.Errorf("blocked feed redirect")
		}
		return nil
	}}
	return []FeedSource{
		&NASAFeed{client, "https://science.nasa.gov/feed/", "nasa-science"},
		&NASAFeed{client, "https://www.nasa.gov/technology/feed/", "nasa-technology"},
	}
}

func (f *NASAFeed) Name() string { return f.ID }

func nasaURL(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && (u.Port() == "" || u.Port() == "443") && (u.Hostname() == "www.nasa.gov" || u.Hostname() == "science.nasa.gov")
}

func (f *NASAFeed) Fetch(ctx context.Context, now time.Time) ([]Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.Endpoint, nil)
	if err != nil {
		return nil, err
	}
	if !nasaURL(req.URL) {
		return nil, fmt.Errorf("blocked feed host")
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/xml")
	res, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var seconds int
		_, _ = fmt.Sscan(res.Header.Get("Retry-After"), &seconds)
		delay := time.Duration(max(0, min(seconds, 86400))) * time.Second
		if stamp, e := http.ParseTime(res.Header.Get("Retry-After")); e == nil {
			delay = max(0, min(stamp.Sub(now), 24*time.Hour))
		}
		return nil, &upstreamError{status: res.StatusCode, retryAfter: delay}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 2<<20 {
		return nil, fmt.Errorf("feed exceeds limit")
	}
	return parseNASAFeed(body, f.ID, now)
}

func feedID(source string) string { return fmt.Sprintf("nasa-%x", sha256.Sum256([]byte(source))) }

func parseNASAFeed(body []byte, provider string, now time.Time) ([]Item, error) {
	var rss struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title       string   `xml:"title"`
				Link        string   `xml:"link"`
				Description string   `xml:"description"`
				Date        string   `xml:"pubDate"`
				Categories  []string `xml:"category"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(body, &rss); err != nil {
		return nil, fmt.Errorf("invalid RSS")
	}
	if rss.Channel.Title == "" || len(rss.Channel.Items) > 100 {
		return nil, fmt.Errorf("invalid feed channel or item count")
	}
	var items []Item
	seen := map[string]bool{}
	for _, entry := range rss.Channel.Items {
		u, err := url.Parse(strings.TrimSpace(entry.Link))
		if err != nil || !nasaURL(u) || u.Path == "" || u.Path == "/" {
			continue
		}
		u.RawQuery, u.Fragment = "", ""
		published, err := time.Parse(time.RFC1123Z, entry.Date)
		if err != nil {
			published, err = time.Parse(time.RFC1123, entry.Date)
		}
		if err != nil || !published.After(now.Add(-Retention)) || published.After(now) {
			continue
		}
		// APOD's feed descriptions contain navigation boilerplate and third-party
		// credits. Do not import those or embedded executable/style content.
		lower := strings.ToLower(entry.Description)
		if strings.HasPrefix(entry.Title, "APOD:") || strings.Contains(lower, "<script") || strings.Contains(lower, "<style") {
			continue
		}
		title, summary := plain(entry.Title), plain(entry.Description)
		if title == "" || len([]rune(title)) > 240 || len(strings.Fields(summary)) < 12 || len([]rune(summary)) > 2000 {
			continue
		}
		source := u.String()
		id := feedID(source)
		if seen[id] {
			continue
		}
		category := "Science"
		if provider == "nasa-technology" {
			category = "Technology"
		}
		for _, raw := range entry.Categories {
			c := strings.ToLower(raw)
			if strings.Contains(c, "galax") || strings.Contains(c, "solar system") || strings.Contains(c, "star") {
				category = "Space"
				break
			}
			if strings.Contains(c, "earth") || strings.Contains(c, "ocean") {
				category = "Nature"
			}
		}
		i := Item{ID: id, Kind: "fact", Title: title, Summary: summary, Category: category,
			SourceName: "NASA", SourceURL: source, FetchedAt: now.UTC(),
			Feed:     &FeedInfo{Provider: provider, PublishedAt: published.UTC(), Validation: "source-excerpt-v1"},
			Sections: []Section{{Heading: "From NASA", Text: summary}}, Sources: []Source{{Name: "NASA: " + title, URL: source}}}
		if err := ValidateFeedItem(i, provider, now); err != nil {
			continue
		}
		seen[id] = true
		items = append(items, i)
	}
	return items, nil
}

// ValidateFeedItem is the publication gate, also enforced by the store. A feed
// cannot smuggle a quiz, image, human-review claim, or unrelated source identity.
func ValidateFeedItem(i Item, provider string, now time.Time) error {
	u, err := url.Parse(i.SourceURL)
	if err != nil || !nasaURL(u) || i.ID != feedID(i.SourceURL) || i.Kind != "fact" || i.Feed == nil || i.Feed.Provider != provider || (provider != "nasa-science" && provider != "nasa-technology") {
		return fmt.Errorf("invalid feed identity")
	}
	if i.Round != nil || i.Photo.URL != "" || i.ReviewedAt != "" || i.ArticleTitle != "" || i.Feed.Validation != "source-excerpt-v1" || i.FetchedAt.IsZero() || i.FetchedAt.After(now) || !i.Feed.PublishedAt.After(now.Add(-Retention)) || i.Feed.PublishedAt.After(now) {
		return fmt.Errorf("invalid feed metadata")
	}
	return i.Validate()
}
