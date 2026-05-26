package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

type projectInput struct {
	Title      string `json:"title"`
	Slug       string `json:"slug"`
	Client     string `json:"client"`
	Summary    string `json:"summary"`
	Body       string `json:"body"`
	CoverImage string `json:"coverImage"`
	Results    string `json:"results"`
	Category   string `json:"category"`
	SortOrder  int    `json:"sortOrder"`
	Status     string `json:"status"`
}

func (a *API) listPublicProjects(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListProjects(r.Context(), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) getPublicProject(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetProjectBySlug(r.Context(), chi.URLParam(r, "slug"), true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) listAdminProjects(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListProjects(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) getAdminProject(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetProjectByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) {
	var in projectInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	item := &store.Project{
		Title: in.Title, Slug: in.Slug, Client: in.Client, Summary: in.Summary,
		Body: sanitizeHTML(in.Body), CoverImage: in.CoverImage, Results: in.Results,
		Category: in.Category, SortOrder: in.SortOrder, Status: in.Status,
	}
	if err := a.store.CreateProject(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) updateProject(w http.ResponseWriter, r *http.Request) {
	existing, err := a.store.GetProjectByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in projectInput
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
	existing.Client = in.Client
	existing.Summary = in.Summary
	existing.Body = sanitizeHTML(in.Body)
	existing.CoverImage = in.CoverImage
	existing.Results = in.Results
	existing.Category = in.Category
	existing.SortOrder = in.SortOrder
	existing.Status = in.Status
	if err := a.store.UpdateProject(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteProject(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
