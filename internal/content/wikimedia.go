package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const UserAgent = "Curio/0.2 (https://github.com/Venkatpandey/curio; self-hosted discovery cache)"
const maxPhotoBytes = 4 << 20

type Media struct {
	Key, ContentType string
	Body             []byte
}
type Cache interface {
	PutContent(context.Context, Item, *Media) error
	FreshArticle(context.Context, string, time.Time) (bool, error)
}
type Place struct{ Title, Region, Category string }

// A bounded geographical catalogue keeps random biographies, lists, maps and
// disambiguation pages out of place discovery. The article text and photos are live.
var Places = []Place{
	{"Deception Island", "South Shetland Islands · Antarctica", "Geography"},
	{"Aogashima", "Izu Islands · Japan", "Geography"},
	{"Surtsey", "Southern coast · Iceland", "Nature"},
	{"Lençóis Maranhenses National Park", "Maranhão · Brazil", "Nature"},
	{"Lake Baikal", "Siberia · Russia", "Geography"},
	{"Danakil Depression", "Afar region · Ethiopia", "Geography"},
	{"Great Blue Hole", "Lighthouse Reef · Belize", "Nature"},
	{"Mount Roraima", "Guiana Highlands · South America", "Geography"},
	{"Derinkuyu underground city", "Cappadocia · Türkiye", "History"},
	{"Skellig Michael", "County Kerry · Ireland", "History"},
	{"Lalibela", "Amhara · Ethiopia", "History"},
	{"Lake Hillier", "Middle Island · Australia", "Nature"},
	{"Tsingy de Bemaraha Strict Nature Reserve", "Western Madagascar", "Nature"},
	{"Raja Ampat Islands", "Southwest Papua · Indonesia", "Nature"},
	{"Salar de Uyuni", "Potosí · Bolivia", "Geography"},
	{"Lake Natron", "Arusha · Tanzania", "Nature"},
	{"Göbekli Tepe", "Şanlıurfa · Türkiye", "History"},
	{"Waitomo Glowworm Caves", "North Island · New Zealand", "Nature"},
	{"Zhangye National Geopark", "Gansu · China", "Geography"},
	{"Canaima National Park", "Bolívar · Venezuela", "Nature"},
	{"Giant's Causeway", "County Antrim · Northern Ireland", "Geography"},
	{"Pamukkale", "Denizli · Türkiye", "Geography"},
	{"Shirakawa-gō and Gokayama", "Central Honshu · Japan", "History"},
	{"Meteora", "Thessaly · Greece", "History"},
}

type Wikimedia struct {
	Client                   *http.Client
	WikipediaURL, CommonsURL string
	cache                    Cache
	logger                   *slog.Logger
}

func NewWikimedia(cache Cache, logger *slog.Logger) *Wikimedia {
	return &Wikimedia{Client: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !allowedHost(req.URL) {
			return fmt.Errorf("blocked upstream redirect")
		}
		return nil
	}}, WikipediaURL: "https://en.wikipedia.org/w/api.php", CommonsURL: "https://commons.wikimedia.org/w/api.php", cache: cache, logger: logger}
}
func allowedHost(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && (u.Port() == "" || u.Port() == "443") && (u.Hostname() == "en.wikipedia.org" || u.Hostname() == "commons.wikimedia.org" || u.Hostname() == "upload.wikimedia.org" || u.Hostname() == "thumb.wikimedia.org")
}

// Run refreshes one catalogue entry at a time. API outages never block startup
// or a discovery request; failures trigger bounded backoff and retain old content.
func (w *Wikimedia) Run(ctx context.Context) {
	delay := time.Duration(0)
	index := 0
	failures := 0
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		place := Places[index%len(Places)]
		index++
		fresh, err := w.cache.FreshArticle(ctx, place.Title, time.Now().Add(-7*24*time.Hour))
		if err != nil {
			w.logger.Warn("content cache lookup failed", "error", err)
			delay = time.Minute
			continue
		}
		if fresh {
			delay = 10 * time.Second
			continue
		}
		requestCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		item, media, err := w.Fetch(requestCtx, place)
		if err == nil {
			err = w.cache.PutContent(requestCtx, item, &media)
		}
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			delay = time.Duration(min(300, 15*(1<<min(failures, 4)))) * time.Second
			var upstream *upstreamError
			if errors.As(err, &upstream) && upstream.retryAfter > delay {
				delay = upstream.retryAfter
			}
			w.logger.Warn("Wikimedia refresh deferred", "article", place.Title, "error", err)
			continue
		}
		failures = 0
		delay = 10 * time.Second
		w.logger.Info("Wikimedia place cached", "article", place.Title)
	}
}
func (w *Wikimedia) getJSON(ctx context.Context, endpoint string, params url.Values, out any) error {
	params.Set("action", "query")
	params.Set("format", "json")
	params.Set("formatversion", "2")
	params.Set("maxlag", "5")
	body, _, err := w.get(ctx, endpoint+"?"+params.Encode(), 2<<20)
	if err != nil {
		return err
	}
	var apiError struct {
		Error *struct{ Code, Info string } `json:"error"`
	}
	if err = json.Unmarshal(body, &apiError); err != nil {
		return err
	}
	if apiError.Error != nil {
		return fmt.Errorf("Wikimedia %s", apiError.Error.Code)
	}
	return json.Unmarshal(body, out)
}
func (w *Wikimedia) get(ctx context.Context, raw string, limit int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	response, err := w.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		retryAfter := time.Duration(0)
		if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
			retryAfter = time.Duration(min(seconds, 86400)) * time.Second
		} else if date, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil {
			retryAfter = time.Until(date)
		}
		return nil, "", &upstreamError{status: response.StatusCode, retryAfter: retryAfter}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > limit {
		return nil, "", fmt.Errorf("upstream response exceeds limit")
	}
	return body, response.Header.Get("Content-Type"), nil
}

type articleResponse struct {
	Query struct {
		Pages []struct {
			PageID                    int `json:"pageid"`
			Title, Extract, PageImage string
			Missing                   bool
			Coordinates               []struct {
				Lat, Lon float64
				Globe    string
			}
			Images []struct{ Title string }
		}
	}
}

func (w *Wikimedia) Fetch(ctx context.Context, place Place) (Item, Media, error) {
	var response articleResponse
	err := w.getJSON(ctx, w.WikipediaURL, url.Values{"titles": {place.Title}, "redirects": {"1"}, "prop": {"extracts|coordinates|pageimages|images"}, "explaintext": {"1"}, "piprop": {"name"}, "imlimit": {"50"}}, &response)
	if err != nil {
		return Item{}, Media{}, err
	}
	if len(response.Query.Pages) != 1 {
		return Item{}, Media{}, fmt.Errorf("article missing")
	}
	p := response.Query.Pages[0]
	if p.Missing || p.PageID <= 0 || len(p.Coordinates) == 0 || p.Coordinates[0].Globe != "earth" {
		return Item{}, Media{}, fmt.Errorf("article is not a geographic place")
	}
	sections := extractSections(p.Extract)
	words := 0
	for _, s := range sections {
		words += len(strings.Fields(s.Text))
	}
	if words < 180 {
		return Item{}, Media{}, fmt.Errorf("article has insufficient readable content")
	}
	titles := []string{"File:" + p.PageImage}
	for _, i := range p.Images {
		titles = append(titles, i.Title)
	}
	var photo Photo
	var media Media
	found := false
	tried := 0
	seen := map[string]bool{}
	for _, title := range titles {
		if !photoFilename(title) || !subjectMatch(title, p.Title) || seen[title] {
			continue
		}
		seen[title] = true
		tried++
		if tried > 3 {
			break
		}
		photo, media, err = w.fetchPhoto(ctx, title, p.Title)
		if err == nil {
			found = true
			break
		}
	}
	if !found {
		if err == nil {
			return Item{}, Media{}, fmt.Errorf("no matching reusable photograph")
		}
		return Item{}, Media{}, fmt.Errorf("no reusable photograph: %v", err)
	}
	intro := sections[0].Text
	remaining := sections[1:]
	introSentences := sentences.FindAllString(intro, -1)
	split := len(introSentences)
	wordCount := 0
	for n, sentence := range introSentences {
		wordCount += len(strings.Fields(sentence))
		if wordCount >= 65 {
			split = n + 1
			break
		}
	}
	if split < len(introSentences) {
		intro = strings.TrimSpace(strings.Join(introSentences[:split], ""))
		remaining = append([]Section{{Heading: "A closer look", Text: strings.TrimSpace(strings.Join(introSentences[split:], ""))}}, remaining...)
	}
	source := "https://en.wikipedia.org/wiki/" + url.PathEscape(strings.ReplaceAll(p.Title, " ", "_"))
	lat, lon := p.Coordinates[0].Lat, p.Coordinates[0].Lon
	item := Item{ID: fmt.Sprintf("wiki-%d", p.PageID), Kind: "place", Title: p.Title, Summary: intro, Sections: remaining, Category: place.Category, Region: place.Region, SourceName: "Wikipedia contributors", SourceURL: source, ArticleTitle: place.Title, TextLicense: "CC BY-SA 4.0", FetchedAt: time.Now().UTC(), Latitude: &lat, Longitude: &lon, Photo: photo, Sources: []Source{{Name: "Wikipedia: " + p.Title, URL: source}}}
	item.Facts = []Fact{{Value: fmt.Sprintf("%.3f°", lat), Label: "Latitude"}, {Value: fmt.Sprintf("%.3f°", lon), Label: "Longitude"}}
	if len(item.Sections) == 0 {
		return Item{}, Media{}, fmt.Errorf("article lacks reading sections")
	}
	if err = item.Validate(); err != nil {
		return Item{}, Media{}, err
	}
	return item, media, nil
}

var tags = regexp.MustCompile(`<[^>]*>`)
var citation = regexp.MustCompile(`\[(?:\d+|citation needed|clarification needed)\]|\(\s*\)`)
var sentences = regexp.MustCompile(`(?s).+?[.!?](?:\s+|$)`)

func plain(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tags.ReplaceAllString(s, ""))), " ")
}
func extractSections(text string) []Section {
	result := []Section{}
	total := 0
	heading := "A closer look"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "==") {
			heading = strings.Trim(line, "= ")
			if heading == "References" || heading == "External links" || heading == "See also" {
				break
			}
			continue
		}
		line = strings.TrimSpace(citation.ReplaceAllString(line, ""))
		if len(strings.Fields(line)) < 15 {
			continue
		}
		if total+len(strings.Fields(line)) > 360 {
			selected := ""
			for _, sentence := range sentences.FindAllString(line, -1) {
				if total+len(strings.Fields(selected+sentence)) > 360 {
					break
				}
				selected += sentence
			}
			line = strings.TrimSpace(selected)
		}
		if line == "" {
			break
		}
		result = append(result, Section{Heading: heading, Text: line})
		total += len(strings.Fields(line))
		heading = "More to notice"
		if total >= 240 {
			break
		}
	}
	// A long opening paragraph can still make an excellent short read. Split at
	// complete sentences, never in the middle of a factual statement.
	if len(result) == 1 {
		parts := sentences.FindAllString(result[0].Text, -1)
		if len(parts) >= 4 {
			middle := len(parts) / 2
			result = []Section{{Heading: "A closer look", Text: strings.TrimSpace(strings.Join(parts[:middle], ""))}, {Heading: "More to notice", Text: strings.TrimSpace(strings.Join(parts[middle:], ""))}}
		}
	}
	return result
}
func photoFilename(s string) bool {
	name := strings.ToLower(s)
	if !(strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") || strings.HasSuffix(name, ".png")) {
		return false
	}
	for _, word := range []string{"flag", "locator", "map", "coat_of_arms", "coat of arms", "logo", "diagram", "icon", "seal of", "relief", "blank", "signature"} {
		if strings.Contains(name, word) {
			return false
		}
	}
	return true
}

type imageResponse struct {
	Query struct {
		Pages []struct {
			ImageInfo []struct {
				URL            string
				ThumbURL       string
				DescriptionURL string
				Mime           string
				ExtMetadata    map[string]metadataField
			} `json:"imageinfo"`
		}
	}
}

func (w *Wikimedia) fetchPhoto(ctx context.Context, title, subject string) (Photo, Media, error) {
	var response imageResponse
	if err := w.getJSON(ctx, w.CommonsURL, url.Values{"titles": {title}, "prop": {"imageinfo"}, "iiprop": {"url|extmetadata|mime"}, "iiurlwidth": {"1280"}}, &response); err != nil {
		return Photo{}, Media{}, err
	}
	if len(response.Query.Pages) != 1 || len(response.Query.Pages[0].ImageInfo) == 0 {
		return Photo{}, Media{}, fmt.Errorf("Commons photograph missing")
	}
	info := response.Query.Pages[0].ImageInfo[0]
	metadata := info.ExtMetadata
	license := plain(metadata["LicenseShortName"].Value)
	artist := plain(metadata["Artist"].Value)
	if !(strings.HasPrefix(license, "CC BY ") || strings.HasPrefix(license, "CC BY-SA ") || license == "CC0" || license == "Public domain") || artist == "" || len(artist) > 1000 || metadata["Restrictions"].Value != "" {
		return Photo{}, Media{}, fmt.Errorf("unsupported image licence")
	}
	description := plain(metadata["ImageDescription"].Value)
	if !subjectMatch(title+" "+description, subject) {
		return Photo{}, Media{}, fmt.Errorf("photograph does not identify this place")
	}
	lower := strings.ToLower(description)
	for _, word := range []string{"locator map", "location map", "coat of arms", "artist's impression", "artistic impression", "illustration of", "satellite map"} {
		if strings.Contains(lower, word) {
			return Photo{}, Media{}, fmt.Errorf("image is not a photograph")
		}
	}
	raw := info.ThumbURL
	if raw == "" {
		raw = info.URL
	}
	u, err := url.Parse(raw)
	if err != nil || !allowedHost(u) || (u.Hostname() != "upload.wikimedia.org" && u.Hostname() != "thumb.wikimedia.org") {
		return Photo{}, Media{}, fmt.Errorf("blocked media host")
	}
	u.RawQuery = ""
	data, _, err := w.get(ctx, u.String(), maxPhotoBytes)
	if err != nil {
		return Photo{}, Media{}, err
	}
	dimensions, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || dimensions.Width > 6000 || dimensions.Height > 6000 || dimensions.Width < 400 {
		return Photo{}, Media{}, fmt.Errorf("unsupported photograph format or dimensions")
	}
	hash := sha256.Sum256(data)
	key := hex.EncodeToString(hash[:])
	mime := "image/" + format
	licenseURL := metadata["LicenseUrl"].Value
	if licenseURL == "" && license == "Public domain" {
		licenseURL = "https://creativecommons.org/publicdomain/mark/1.0/"
	}
	licenseURL = strings.Replace(licenseURL, "http://", "https://", 1)
	parsedLicense, err := url.Parse(licenseURL)
	if err != nil || parsedLicense.Scheme != "https" || parsedLicense.Hostname() != "creativecommons.org" {
		return Photo{}, Media{}, fmt.Errorf("missing licence link")
	}
	sourceURL, err := url.Parse(info.DescriptionURL)
	if err != nil || sourceURL.Scheme != "https" || sourceURL.Hostname() != "commons.wikimedia.org" {
		return Photo{}, Media{}, fmt.Errorf("missing image source")
	}
	caption := description
	if len([]rune(caption)) > 350 {
		caption = string([]rune(caption)[:347]) + "…"
	}
	if caption == "" {
		caption = subject + ". See the image source for details."
	}
	return Photo{Key: key, URL: "/media/" + key, SourceURL: info.DescriptionURL, Artist: artist, License: license, LicenseURL: licenseURL, Caption: caption, Alt: "Photograph accompanying " + subject + "; " + plain(strings.TrimPrefix(title, "File:"))}, Media{Key: key, ContentType: mime, Body: data}, nil
}

// NumericHighlights preserves complete number-bearing source sentences for the
// reading sidebar; it never invents labels or drops units from source claims.
func (i Item) NumericHighlights() []string {
	var out []string
	digit := regexp.MustCompile(`\d`)
	text := i.Summary
	for _, s := range i.Sections {
		text += " " + s.Text
	}
	for _, s := range sentences.FindAllString(text, -1) {
		if digit.MatchString(s) && len(strings.Fields(s)) <= 45 {
			out = append(out, strings.TrimSpace(s))
			if len(out) == 2 {
				break
			}
		}
	}
	return out
}

type upstreamError struct {
	status     int
	retryAfter time.Duration
}

func (e *upstreamError) Error() string { return fmt.Sprintf("upstream HTTP %d", e.status) }

// Commons includes numeric extension versions alongside textual credit fields.
// Unknown scalar types are ignored; required attribution is validated below.
type metadataField struct{ Value string }

func (m *metadataField) UnmarshalJSON(body []byte) error {
	var field struct{ Value json.RawMessage }
	if err := json.Unmarshal(body, &field); err != nil {
		return err
	}
	_ = json.Unmarshal(field.Value, &m.Value)
	return nil
}

func subjectMatch(text, subject string) bool {
	normalize := func(s string) string {
		return strings.NewReplacer("ö", "o", "ó", "o", "ō", "o", "ã", "a", "á", "a", "â", "a", "é", "e", "è", "e", "ç", "c", "ş", "s", "ü", "u", "ğ", "g", "ı", "i", "_", " ").Replace(strings.ToLower(s))
	}
	text = normalize(text)
	subject = normalize(subject)
	// Commons filenames also use this island's Japanese name and alternate
	// romanization. Keep aliases specific to avoid unrelated navigation images.
	if subject == "aogashima" && (strings.Contains(text, "青ヶ島") || strings.Contains(text, "aogasima")) {
		return true
	}
	generic := map[string]bool{"island": true, "islands": true, "lake": true, "mount": true, "mountain": true, "national": true, "park": true, "reserve": true, "nature": true, "strict": true, "caves": true, "cave": true, "city": true, "underground": true, "the": true, "and": true}
	for _, word := range strings.Fields(subject) {
		if len(word) >= 4 && !generic[word] && strings.Contains(text, word) {
			return true
		}
	}
	return false
}
