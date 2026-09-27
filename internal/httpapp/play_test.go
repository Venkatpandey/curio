package httpapp

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPlayRevealValidationAndProfileIsolation(t *testing.T) {
	f := setup(t, true)
	c := f.client()
	csrf := f.csrf(c, "/discover?kind=fact&id=octopus")
	res, body := f.get(c, "/discover?kind=fact&id=octopus")
	if res.StatusCode != 200 || strings.Contains(body, `class="answer-reveal"`) || !strings.Contains(body, "Eight arms. How many hearts?") {
		t.Fatal("answer not hidden")
	}
	values := url.Values{"csrf": {csrf}, "id": {"octopus"}, "kind": {"fact"}, "answer": {"1"}}
	res, body = f.post(c, "/play", values)
	if res.StatusCode != 200 || !strings.Contains(body, "You called it!") || strings.Contains(body, "points banked") {
		t.Fatal("guest reveal", res.StatusCode, body)
	}
	values.Set("answer", "999")
	res, _ = f.post(c, "/play", values)
	if res.StatusCode != 400 {
		t.Fatal("invalid answer accepted")
	}
	values.Set("answer", "0")
	values.Set("csrf", "bad")
	res, _ = f.post(c, "/play", values)
	if res.StatusCode != 403 {
		t.Fatal("csrf bypass")
	}
	f.enter(c, "Alice")
	values.Set("csrf", f.csrf(c, "/discover?kind=fact&id=octopus"))
	res, _ = f.post(c, "/play", values)
	if res.StatusCode != 303 {
		t.Fatal("answer failed")
	}
	res, body = f.get(c, res.Header.Get("Location"))
	if res.StatusCode != 200 || !strings.Contains(body, "Plot twist!") || !strings.Contains(body, "3 points banked") {
		t.Fatal("wrong guess not rewarded")
	}
	values.Set("answer", "1")
	f.post(c, "/play", values)
	uid, err := f.db.EnterUser(context.Background(), "Alice")
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.db.Progress(context.Background(), uid, time.Now())
	if err != nil || p.Points != 3 || p.Correct != 0 {
		t.Fatal("retry changed score", p, err)
	}
	bob := f.client()
	f.enter(bob, "Bob")
	_, body = f.get(bob, "/discover?kind=fact&id=octopus")
	if strings.Contains(body, `class="answer-reveal"`) {
		t.Fatal("attempt leaked")
	}
	res, _ = f.post(c, "/react", url.Values{"csrf": {values.Get("csrf")}, "id": {"octopus"}, "reaction": {"interesting"}})
	if res.StatusCode != 303 {
		t.Fatal("reaction failed")
	}
	res, _ = f.get(c, "/discover?kind=fact&category=Animals")
	if res.StatusCode != 303 || !strings.Contains(res.Header.Get("Location"), "category=Animals") {
		t.Fatal("category filter failed")
	}
	res, _ = f.get(c, "/discover?kind=fact&category=invalid")
	if res.StatusCode != 400 {
		t.Fatal("bad category accepted")
	}
}
