package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

// subscribeNewsletter records a public newsletter signup.
func (a *API) subscribeNewsletter(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if err := a.store.AddSubscriber(r.Context(), email); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"message": "You're subscribed — watch your inbox for our next insight.",
	})
}

// listSubscribers returns every newsletter subscriber for the admin view.
func (a *API) listSubscribers(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListSubscribers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// deleteSubscriber removes a subscriber.
func (a *API) deleteSubscriber(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteSubscriber(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "subscriber not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
