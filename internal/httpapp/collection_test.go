package httpapp

import (
	"net/url"
	"strings"
	"testing"
)

func TestPrivateCollectionAndFavoriteActions(t *testing.T) {
	f := setup(t, true)
	guest := f.client()
	for _, path := range []string{"/collection", "/collection?tab=history", "/collection?tab=mix"} {
		r, _ := f.get(guest, path)
		if r.StatusCode != 303 || r.Header.Get("Location") != "/enter" {
			t.Fatal("private collection accessible")
		}
	}
	c := f.client()
	f.enter(c, "Alice")
	csrf := f.csrf(c, "/discover?kind=fact&id=venus")
	form := url.Values{"csrf": {csrf}, "id": {"venus"}, "action": {"save"}, "return": {"https://evil.example"}}
	r, _ := f.post(c, "/favorite", form)
	if r.StatusCode != 303 || !strings.HasPrefix(r.Header.Get("Location"), "/discover?") {
		t.Fatal("unsafe favorite redirect")
	}
	_, body := f.get(c, "/collection")
	if !strings.Contains(body, "A year before one full turn.") {
		t.Fatal("saved story missing")
	}
	bob := f.client()
	f.enter(bob, "Bob")
	_, body = f.get(bob, "/collection")
	if strings.Contains(body, "A year before one full turn.") {
		t.Fatal("favorite leaked")
	}
	form.Set("action", "toggle")
	r, _ = f.post(c, "/favorite", form)
	if r.StatusCode != 400 {
		t.Fatal("invalid action accepted")
	}
	form.Set("action", "remove")
	form.Set("csrf", "bad")
	r, _ = f.post(c, "/favorite", form)
	if r.StatusCode != 403 {
		t.Fatal("favorite csrf missing")
	}
	r, _ = f.post(c, "/reset-mix", url.Values{"csrf": {"bad"}})
	if r.StatusCode != 403 {
		t.Fatal("reset csrf missing")
	}
	r, _ = f.post(c, "/reset-mix", url.Values{"csrf": {csrf}})
	if r.StatusCode != 303 {
		t.Fatal("reset failed")
	}
	_, body = f.get(c, "/collection")
	if !strings.Contains(body, "A year before one full turn.") {
		t.Fatal("reset removed collection")
	}
	form.Set("csrf", csrf)
	form.Set("return", "collection")
	r, _ = f.post(c, "/favorite", form)
	if r.StatusCode != 303 {
		t.Fatal("remove failed")
	}
	_, body = f.get(c, "/collection")
	if strings.Contains(body, "A year before one full turn.") {
		t.Fatal("favorite not removed")
	}
	for _, path := range []string{"/collection?tab=invalid", "/collection?page=-1", "/collection?page=100001"} {
		r, _ = f.get(c, path)
		if r.StatusCode != 400 {
			t.Fatal("invalid collection query accepted")
		}
	}
}
