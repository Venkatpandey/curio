package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func responseJSON(t *testing.T, v any) *http.Response {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(b))}
}
func fixtureWikimedia(t *testing.T, license, photoURL string) *Wikimedia {
	t.Helper()
	w := NewWikimedia(nil, slog.Default())
	var picture bytes.Buffer
	jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 600, 400)), nil)
	// Synthetic fixture paragraphs exercise length handling, not factual content.
	text := strings.Repeat("The island has a lake measuring 6.7 kilometres across, with a shoreline where researchers monitor changes in the surrounding landscape. ", 5) + "\n\n" + strings.Repeat("Visitors can see the shoreline from a path that follows the surrounding hills and passes several rocky viewpoints along the way. ", 7)
	w.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") != UserAgent {
			t.Error("missing identifying user agent")
		}
		switch r.URL.Hostname() {
		case "en.wikipedia.org":
			return responseJSON(t, map[string]any{"query": map[string]any{"pages": []any{map[string]any{"pageid": 123, "title": "Test Island", "extract": text, "pageimage": "Test island.jpg", "coordinates": []any{map[string]any{"lat": 12.3, "lon": 45.6, "globe": "earth"}}}}}}), nil
		case "commons.wikimedia.org":
			return responseJSON(t, map[string]any{"query": map[string]any{"pages": []any{map[string]any{"imageinfo": []any{map[string]any{"thumburl": photoURL, "descriptionurl": "https://commons.wikimedia.org/wiki/File:Test_island.jpg", "extmetadata": map[string]any{"CommonsMetadataExtension": map[string]any{"value": 1.2}, "LicenseShortName": map[string]string{"value": license}, "Artist": map[string]string{"value": "<b>Jane Photographer</b>"}, "LicenseUrl": map[string]string{"value": "https://creativecommons.org/licenses/by/4.0/"}, "ImageDescription": map[string]string{"value": "A photograph of the island."}}}}}}}}), nil
		case "upload.wikimedia.org":
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(picture.Bytes()))}, nil
		default:
			t.Errorf("unexpected outbound host: %s", r.URL.Host)
			return nil, fmt.Errorf("unexpected host")
		}
	})}
	return w
}
func TestWikimediaNormalizesLicensedPhotographs(t *testing.T) {
	w := fixtureWikimedia(t, "CC BY 4.0", "https://upload.wikimedia.org/wikipedia/commons/test.jpg")
	item, media, err := w.Fetch(context.Background(), Place{Title: "Test Island", Region: "Test region", Category: "Geography"})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != "wiki-123" || item.Photo.Artist != "Jane Photographer" || item.Latitude == nil || *item.Latitude != 12.3 {
		t.Fatal("normalization failed", item)
	}
	if media.ContentType != "image/jpeg" || len(media.Key) != 64 || !strings.HasPrefix(item.Photo.URL, "/media/") {
		t.Fatal("media not cached by digest")
	}
	if !strings.Contains(item.Summary, "6.7 kilometres") {
		t.Fatal("decimal damaged in extract")
	}
	if item.ReadingMinutes() < 1 || item.ReadingMinutes() > 2 {
		t.Fatal("unexpected read length")
	}
	if item.TextLicense != "CC BY-SA 4.0" || item.FetchedAt.IsZero() {
		t.Fatal("missing text provenance")
	}
}
func TestWikimediaRejectsUnsafeOrUnlicensedPhotos(t *testing.T) {
	for _, tc := range []struct{ license, url string }{{"All rights reserved", "https://upload.wikimedia.org/photo.jpg"}, {"CC BY 4.0", "http://127.0.0.1/private"}, {"CC BY 4.0", "https://upload.wikimedia.org.evil.example/photo.jpg"}} {
		if _, _, err := fixtureWikimedia(t, tc.license, tc.url).Fetch(context.Background(), Place{Title: "Test Island"}); err == nil {
			t.Fatal("unsafe photo accepted", tc)
		}
	}
	for _, title := range []string{"File:Flag of Iceland.svg", "File:Location map.jpg", "File:Coat of arms.png", "File:diagram.png"} {
		if photoFilename(title) {
			t.Fatalf("non-photo accepted: %s", title)
		}
	}
}
func TestWikimediaNetworkFailureAndSizeLimit(t *testing.T) {
	w := NewWikimedia(nil, slog.Default())
	w.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("rate limited"))}, nil
	})}
	if _, _, err := w.Fetch(context.Background(), Place{Title: "Test Island"}); err == nil {
		t.Fatal("provider ignored 429")
	}
	w.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("too large"))}, nil
	})}
	if _, _, err := w.get(context.Background(), "https://en.wikipedia.org/", 3); err == nil {
		t.Fatal("size limit ignored")
	}
}
func TestExtractPreservesDecimalSentences(t *testing.T) {
	text := strings.Repeat("An island measuring 6.7 kilometres across is surrounded by a ring of cliffs and a broad area of open water. ", 15)
	sections := extractSections(text)
	if len(sections) < 2 {
		t.Fatal("single paragraph not split")
	}
	for _, section := range sections {
		if strings.HasPrefix(section.Text, "7 kilometres") {
			t.Fatal("number truncated")
		}
	}
}
func TestLongFormStoriesHavePhotosAndReadingDepth(t *testing.T) {
	for _, item := range Items {
		if err := item.Validate(); err != nil {
			t.Fatal(item.ID, err)
		}
		if item.Round != nil {
			continue
		}
		count := len(strings.Fields(item.Summary))
		for _, s := range item.Sections {
			count += len(strings.Fields(s.Text))
		}
		if count < 200 || count > 440 {
			t.Fatalf("%s is %d words", item.ID, count)
		}
		if len(item.Facts) < 3 || len(item.Sources) < 2 || !strings.HasPrefix(item.Photo.URL, "/static/photos/") {
			t.Fatal("incomplete editorial story", item.ID)
		}
	}
	if km := DistanceKM(0, 0, 0, 1); km != 111 {
		t.Fatal("distance calculation", km)
	}
}

func TestPhotographMustMatchPlace(t *testing.T) {
	if subjectMatch("South Pole marker", "Deception Island") {
		t.Fatal("unrelated navigation photo matched place")
	}
	if !subjectMatch("Deception_island.jpg", "Deception Island") {
		t.Fatal("place photograph rejected")
	}
	if !subjectMatch("Gobekli Tepe ruins.jpg", "Göbekli Tepe") {
		t.Fatal("accented place name failed")
	}
	for _, name := range []string{"File:青ヶ島.jpg", "File:Aogasima maruyama.jpg"} {
		if !subjectMatch(name, "Aogashima") {
			t.Fatal("local place name rejected", name)
		}
	}
}
