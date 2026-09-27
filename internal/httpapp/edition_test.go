package httpapp

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"curio/internal/content"
	"curio/internal/store"
)

func TestFreshnessAccessAndNewArrivals(t *testing.T) {
	f := setup(t, false)
	c := f.client()
	r, _ := f.get(c, "/freshness")
	if r.StatusCode != 303 {
		t.Fatal("private catalogue exposed")
	}
	f.enter(c, "Fresh")
	r, body := f.get(c, "/freshness")
	var catalogue store.Catalogue
	if r.StatusCode != 200 || r.Header.Get("Cache-Control") != "no-store" || json.Unmarshal([]byte(body), &catalogue) != nil || catalogue.Count != 50 {
		t.Fatal("freshness response", body)
	}
	_, body = f.get(c, "/")
	if strings.Contains(body, "new discoveries since") {
		t.Fatal("first visit incorrectly claims arrivals")
	}
	item := content.Items[0]
	item.ID = "arrived-today"
	if err := f.db.PutContent(context.Background(), item, nil); err != nil {
		t.Fatal(err)
	}
	_, body = f.get(c, "/")
	if !strings.Contains(body, "1 new discovery since") {
		t.Fatal("missing arrival count")
	}
	_, body = f.get(c, "/")
	if strings.Contains(body, "new discoveries since") {
		t.Fatal("arrival count did not reset")
	}
}
func TestNewFormatsScoreOnServerAndKeepTopic(t *testing.T) {
	f := setup(t, true)
	c := f.client()
	for _, tc := range []struct{ id, category, format, answer string }{
		{"water-peak", "Science", "true-false", "0"},
		{"liberty-hand", "History", "comparison", "0"},
	} {
		path := "/discover?kind=fact&id=" + tc.id + "&category=" + tc.category
		csrf := f.csrf(c, path)
		_, body := f.get(c, path)
		if !strings.Contains(body, `data-format="`+tc.format+`"`) || strings.Contains(body, `class="fact-reveal-art"`) {
			t.Fatal("format/reveal state", tc.id)
		}
		r, body := f.post(c, "/play", url.Values{"csrf": {csrf}, "id": {tc.id}, "kind": {"fact"}, "category": {tc.category}, "answer": {tc.answer}, "correct": {"false"}})
		if r.StatusCode != 200 || !strings.Contains(body, "You called it!") || !strings.Contains(body, "&amp;category="+tc.category) {
			t.Fatal("guest round", tc.id, body)
		}
	}
	f.enter(c, "NewPlayer")
	csrf := f.csrf(c, "/discover?kind=fact&id=water-peak")
	values := url.Values{"csrf": {csrf}, "id": {"water-peak"}, "kind": {"fact"}, "category": {"Science"}, "answer": {"0"}}
	r, _ := f.post(c, "/play", values)
	if r.StatusCode != 303 || !strings.Contains(r.Header.Get("Location"), "category=Science") {
		t.Fatal("topic lost after answer")
	}
	_, body := f.get(c, r.Header.Get("Location"))
	if !strings.Contains(body, "3 points banked") {
		t.Fatal("new format did not save score")
	}
	values.Set("answer", "1")
	f.post(c, "/play", values)
	_, body = f.get(c, "/discover?kind=fact&id=water-peak")
	if !strings.Contains(body, "You called it!") {
		t.Fatal("replay changed saved answer")
	}
}
