package httpapp

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"curio/internal/store"
)

type guestAttemptKey struct{}

func (a *App) play(w http.ResponseWriter, r *http.Request) {
	if !a.mayBrowse(w, r) {
		return
	}
	item, err := a.store.Content(r.Context(), r.PostForm.Get("id"))
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	answer, err := strconv.Atoi(r.PostForm.Get("answer"))
	quiz := item.Quiz()
	if err != nil || answer < -1 || answer >= len(quiz.Options) {
		http.Error(w, "Choose an answer or reveal the story.", 400)
		return
	}
	correct := answer >= 0 && answer == quiz.Answer
	kind := r.PostForm.Get("kind")
	if kind != item.Kind && kind != "surprise" {
		kind = item.Kind
	}
	query := url.Values{"kind": {kind}, "id": {item.ID}}
	if category := r.PostForm.Get("category"); kind == "fact" && (category == "Space" || category == "Animals") {
		query.Set("category", category)
	}
	if state := stateOf(r); state.session != nil {
		if err = a.store.Complete(r.Context(), state.session.User.ID, item.ID, answer, correct, time.Now()); err != nil {
			a.fail(w, r, err)
			return
		}
		http.Redirect(w, r, "/discover?"+query.Encode()+"#reveal", http.StatusSeeOther)
		return
	}
	// Guests can play without creating a profile; no score is claimed as saved.
	r = r.WithContext(context.WithValue(r.Context(), guestAttemptKey{}, &store.Attempt{Answer: answer, Correct: correct}))
	r.URL.RawQuery = query.Encode()
	a.discover(w, r)
}
func (a *App) react(w http.ResponseWriter, r *http.Request) {
	user := a.requireUser(w, r)
	if user == nil {
		return
	}
	id := r.PostForm.Get("id")
	item, err := a.store.Content(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	value := r.PostForm.Get("reaction")
	if value != "interesting" && value != "not-for-me" && value != "clear" {
		http.Error(w, "Unknown reaction.", 400)
		return
	}
	if err = a.store.React(r.Context(), user.ID, id, value); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/discover?kind="+item.Kind+"&id="+url.QueryEscape(id)+"#reactions", 303)
}
