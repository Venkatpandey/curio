package content

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"strings"
	"time"
)

type Fact struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Note  string `json:"note,omitempty"`
}
type Section struct {
	Heading string `json:"heading"`
	Text    string `json:"text"`
}
type Source struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}
type Photo struct {
	Key        string `json:"key,omitempty"`
	URL        string `json:"url"`
	SourceURL  string `json:"source_url"`
	Artist     string `json:"artist"`
	License    string `json:"license"`
	LicenseURL string `json:"license_url"`
	Caption    string `json:"caption"`
	Alt        string `json:"alt"`
}
type Item struct {
	ID           string    `json:"id"`
	Feed         *FeedInfo `json:"feed,omitempty"`
	Round        *Quiz     `json:"quiz,omitempty"`
	Kind         string    `json:"kind"`
	Title        string    `json:"title"`
	Summary      string    `json:"summary"`
	Category     string    `json:"category"`
	Region       string    `json:"region"`
	SourceName   string    `json:"source_name"`
	SourceURL    string    `json:"source_url"`
	ReviewedAt   string    `json:"reviewed_at,omitempty"`
	ArticleTitle string    `json:"article_title,omitempty"`
	TextLicense  string    `json:"text_license,omitempty"`
	FetchedAt    time.Time `json:"fetched_at,omitempty"`
	Latitude     *float64  `json:"latitude,omitempty"`
	Longitude    *float64  `json:"longitude,omitempty"`
	Facts        []Fact    `json:"facts"`
	Sections     []Section `json:"sections"`
	Sources      []Source  `json:"sources"`
	Photo        Photo     `json:"photo"`
}

func (i Item) ReadingMinutes() int {
	words := len(strings.Fields(i.Summary))
	for _, s := range i.Sections {
		words += len(strings.Fields(s.Text))
	}
	return max(1, int(math.Ceil(float64(words)/220)))
}
func (i Item) MapURL() string {
	if i.Latitude == nil || i.Longitude == nil {
		return ""
	}
	return fmt.Sprintf("https://www.openstreetmap.org/?mlat=%.6f&mlon=%.6f#map=10/%.6f/%.6f", *i.Latitude, *i.Longitude, *i.Latitude, *i.Longitude)
}
func (i Item) Coordinates() string {
	if i.Latitude == nil || i.Longitude == nil {
		return ""
	}
	return fmt.Sprintf("%.3f°, %.3f°", *i.Latitude, *i.Longitude)
}
func (i Item) Validate() error {
	if i.ID == "" || len(i.ID) > 100 || (i.Kind != "place" && i.Kind != "fact") || i.Title == "" || i.Summary == "" || len(i.Sections) == 0 {
		return fmt.Errorf("incomplete discovery")
	}
	for _, s := range append(i.Sources, Source{Name: i.SourceName, URL: i.SourceURL}) {
		u, err := url.Parse(s.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || s.Name == "" {
			return fmt.Errorf("invalid provenance")
		}
	}
	if (i.Latitude == nil) != (i.Longitude == nil) {
		return fmt.Errorf("incomplete coordinates")
	}
	if i.Latitude != nil && (math.IsNaN(*i.Latitude) || math.IsNaN(*i.Longitude) || math.Abs(*i.Latitude) > 90 || math.Abs(*i.Longitude) > 180) {
		return fmt.Errorf("invalid coordinates")
	}
	if (i.Kind == "place" || i.Photo.URL != "") && (i.Photo.URL == "" || i.Photo.Artist == "" || i.Photo.License == "" || i.Photo.SourceURL == "" || i.Photo.Alt == "") {
		return fmt.Errorf("incomplete photograph attribution")
	}
	if i.Round != nil {
		q := i.Round
		if (q.Format != "true-false" && q.Format != "comparison") || q.Question == "" || len(q.Options) != 2 || q.Answer < 0 || q.Answer >= len(q.Options) || q.Explanation == "" {
			return fmt.Errorf("invalid quiz")
		}
		if strings.TrimSpace(q.Options[0]) == "" || strings.TrimSpace(q.Options[1]) == "" || q.Options[0] == q.Options[1] {
			return fmt.Errorf("invalid quiz options")
		}
		if q.Format == "true-false" && (q.Options[0] != "True" || q.Options[1] != "False") {
			return fmt.Errorf("invalid true-false options")
		}
	}
	return nil
}

func (i Item) Palette() string {
	switch i.Category {
	case "Space":
		return "space"
	case "Animals", "Nature":
		return "nature"
	case "Science", "Technology":
		return "science"
	case "History", "Language", "Food":
		return "amber"
	default:
		return "ocean"
	}
}

type Request struct {
	Kind, ExcludeID, Category string
	UserID                    int64
	RecentIDs                 []string
}
type Provider interface {
	Discover(context.Context, Request) (Item, error)
}
type Starter struct{}

//go:embed data/*.json
var data embed.FS
var Items = loadItems()

func loadItems() []Item {
	var items []Item
	body, err := data.ReadFile("data/stories.json")
	if err != nil {
		panic(err)
	}
	if err = json.Unmarshal(body, &items); err != nil {
		panic(err)
	}
	var photos map[string]Photo
	body, err = data.ReadFile("data/photos.json")
	if err != nil {
		panic(err)
	}
	if err = json.Unmarshal(body, &photos); err != nil {
		panic(err)
	}
	for n := range items {
		caption, alt := items[n].Photo.Caption, items[n].Photo.Alt
		items[n].Photo = photos[items[n].ID]
		items[n].Photo.Caption = caption
		items[n].Photo.Alt = alt
		if err = items[n].Validate(); err != nil {
			panic(err)
		}
	}
	body, err = data.ReadFile("data/quick-facts.json")
	if err != nil {
		panic(err)
	}
	var facts []Item
	if err = json.Unmarshal(body, &facts); err != nil {
		panic(err)
	}
	for _, item := range facts {
		if err = item.Validate(); err != nil {
			panic(fmt.Sprintf("%s: %v", item.ID, err))
		}
	}
	return append(items, facts...)
}
func (Starter) Discover(ctx context.Context, r Request) (Item, error) {
	if err := ctx.Err(); err != nil {
		return Item{}, err
	}
	candidates := []Item{}
	for _, i := range Items {
		if (r.Kind == "surprise" || i.Kind == r.Kind) && i.ID != r.ExcludeID && (r.Category == "" || r.Category == i.Category) {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return Item{}, fmt.Errorf("no discoveries for kind")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(candidates))))
	if err != nil {
		return Item{}, err
	}
	return candidates[n.Int64()], nil
}
func DistanceKM(lat1, lon1, lat2, lon2 float64) int {
	const rad = math.Pi / 180
	dlat := (lat2 - lat1) * rad
	dlon := (lon2 - lon1) * rad
	a := math.Pow(math.Sin(dlat/2), 2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Pow(math.Sin(dlon/2), 2)
	return int(math.Round(6371 * 2 * math.Asin(math.Sqrt(min(1, a)))))
}
