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
	Pillar    string `json:"pillar"`
	SortOrder int    `json:"sortOrder"`
	Status    string `json:"status"`
}

type serviceDetail struct {
	*store.Service
	Subservices []store.ServiceSubservice `json:"subservices"`
}

type serviceSubserviceInput struct {
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	Body      string `json:"body"`
	SortOrder int    `json:"sortOrder"`
	Status    string `json:"status"`
}

func validServicePillar(pillar, status string) bool {
	if status == "draft" && pillar == "" {
		return true
	}
	switch pillar {
	case "brand_identity", "digital_platforms", "content_growth":
		return true
	default:
		return false
	}
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
	subservices, err := a.store.ListServiceSubservices(r.Context(), item.ID, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, serviceDetail{Service: item, Subservices: subservices})
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
	if !validServicePillar(in.Pillar, in.Status) {
		writeError(w, http.StatusBadRequest, "published services require a valid pillar")
		return
	}
	item := &store.Service{
		Title: in.Title, Slug: in.Slug, Summary: in.Summary,
		Body: sanitizeHTML(in.Body),
		Icon: in.Icon, Pillar: in.Pillar, SortOrder: in.SortOrder, Status: in.Status,
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
	if !validServicePillar(in.Pillar, in.Status) {
		writeError(w, http.StatusBadRequest, "published services require a valid pillar")
		return
	}
	existing.Title = in.Title
	existing.Slug = in.Slug
	existing.Summary = in.Summary
	existing.Body = sanitizeHTML(in.Body)
	existing.Icon = in.Icon
	existing.Pillar = in.Pillar
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

func (a *API) listAdminServiceSubservices(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListServiceSubservices(r.Context(), parseID(chi.URLParam(r, "id")), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) createServiceSubservice(w http.ResponseWriter, r *http.Request) {
	var in serviceSubserviceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	item := &store.ServiceSubservice{
		ServiceID: parseID(chi.URLParam(r, "id")), Title: in.Title,
		Summary: in.Summary, Body: sanitizeHTML(in.Body),
		SortOrder: in.SortOrder, Status: in.Status,
	}
	if err := a.store.CreateServiceSubservice(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) updateServiceSubservice(w http.ResponseWriter, r *http.Request) {
	serviceID := parseID(chi.URLParam(r, "id"))
	item, err := a.store.GetServiceSubservice(r.Context(), serviceID, parseID(chi.URLParam(r, "subserviceId")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "subservice not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in serviceSubserviceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	item.Title = in.Title
	item.Summary = in.Summary
	item.Body = sanitizeHTML(in.Body)
	item.SortOrder = in.SortOrder
	item.Status = in.Status
	if err := a.store.UpdateServiceSubservice(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) deleteServiceSubservice(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteServiceSubservice(r.Context(), parseID(chi.URLParam(r, "id")), parseID(chi.URLParam(r, "subserviceId")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "subservice not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
