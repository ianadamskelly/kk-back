package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

type statInput struct {
	Label     string `json:"label"`
	Value     string `json:"value"`
	SortOrder int    `json:"sortOrder"`
}

func (a *API) listStats(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) listAdminStats(w http.ResponseWriter, r *http.Request) {
	a.listStats(w, r)
}

func (a *API) createStat(w http.ResponseWriter, r *http.Request) {
	var in statInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Label == "" || in.Value == "" {
		writeError(w, http.StatusBadRequest, "label and value are required")
		return
	}
	item := &store.Stat{Label: in.Label, Value: in.Value, SortOrder: in.SortOrder}
	if err := a.store.CreateStat(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) updateStat(w http.ResponseWriter, r *http.Request) {
	var in statInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Label == "" || in.Value == "" {
		writeError(w, http.StatusBadRequest, "label and value are required")
		return
	}
	item := &store.Stat{
		ID: parseID(chi.URLParam(r, "id")), Label: in.Label,
		Value: in.Value, SortOrder: in.SortOrder,
	}
	if err := a.store.UpdateStat(r.Context(), item); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "stat not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) deleteStat(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteStat(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "stat not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
