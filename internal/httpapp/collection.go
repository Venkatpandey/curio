package httpapp

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

func (a *App) collection(w http.ResponseWriter, r *http.Request) {
	user := a.requireUser(w, r)
	if user == nil {
		return
	}
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "favorites"
	}
	if tab != "favorites" && tab != "history" && tab != "mix" {
		http.Error(w, "Unknown collection view.", 400)
		return
	}
	number := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		var err error
		number, err = strconv.Atoi(raw)
		if err != nil || number < 1 || number > 100000 {
			http.Error(w, "Invalid page.", 400)
			return
		}
	}
	p := page{Title: "Your collection", View: "collection", CollectionTab: tab, PageNumber: number}
	var err error
	if tab == "mix" {
		p.Preferences, err = a.store.Preferences(r.Context(), user.ID)
	} else {
		p.Collection, p.HasMore, err = a.store.Collection(r.Context(), user.ID, tab, number)
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if number > 1 {
		p.PreviousPage = number - 1
	}
	if p.HasMore {
		p.NextPage = number + 1
	}
	switch r.URL.Query().Get("done") {
	case "saved":
		p.Notice = "Saved to your collection."
	case "removed":
		p.Notice = "Removed from favorites."
	case "reset":
		p.Notice = "Fresh start. Your mix is open to everything again."
	}
	a.render(w, r, 200, p)
}
func (a *App) favorite(w http.ResponseWriter, r *http.Request) {
	user := a.requireUser(w, r)
	if user == nil {
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
	action := r.PostForm.Get("action")
	if action != "save" && action != "remove" {
		http.Error(w, "Choose save or remove.", 400)
		return
	}
	if err = a.store.SaveFavorite(r.Context(), user.ID, item.ID, action == "save"); err != nil {
		a.fail(w, r, err)
		return
	}
	done := "saved"
	if action == "remove" {
		done = "removed"
	}
	if r.PostForm.Get("return") == "collection" {
		tab := r.PostForm.Get("tab")
		if tab != "history" {
			tab = "favorites"
		}
		http.Redirect(w, r, "/collection?tab="+tab+"&done="+done, 303)
		return
	}
	kind := r.PostForm.Get("kind")
	if kind != item.Kind && kind != "surprise" {
		kind = item.Kind
	}
	http.Redirect(w, r, "/discover?kind="+kind+"&id="+url.QueryEscape(item.ID)+"&saved="+done+"#keep-story", 303)
}
func (a *App) resetMix(w http.ResponseWriter, r *http.Request) {
	user := a.requireUser(w, r)
	if user == nil {
		return
	}
	if err := a.store.ResetPreferences(r.Context(), user.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/collection?tab=mix&done=reset", 303)
}
