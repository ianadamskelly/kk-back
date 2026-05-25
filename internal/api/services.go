package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

type serviceInput struct {
	Title     string `json:"title"`
	Slug      string `json:"slug"`
	Summary   string `json:"summary"`
	Body      string `json:"body"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sortOrder"`
	Status    string `json:"status"`
}

func (a *API) listPublicServices(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListServices(r.Context(), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) getPublicService(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetServiceBySlug(r.Context(), chi.URLParam(r, "slug"), true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "service not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) listAdminServices(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListServices(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) getAdminService(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetServiceByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "service not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) createService(w http.ResponseWriter, r *http.Request) {
	var in serviceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	item := &store.Service{
		Title: in.Title, Slug: in.Slug, Summary: in.Summary, Body: in.Body,
		Icon: in.Icon, SortOrder: in.SortOrder, Status: in.Status,
	}
	if err := a.store.CreateService(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) updateService(w http.ResponseWriter, r *http.Request) {
	existing, err := a.store.GetServiceByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "service not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in serviceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	existing.Title = in.Title
	existing.Slug = in.Slug
	existing.Summary = in.Summary
	existing.Body = in.Body
	existing.Icon = in.Icon
	existing.SortOrder = in.SortOrder
	existing.Status = in.Status
	if err := a.store.UpdateService(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (a *API) deleteService(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteService(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "service not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
