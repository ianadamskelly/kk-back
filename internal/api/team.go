package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

type teamInput struct {
	Name      string            `json:"name"`
	Role      string            `json:"role"`
	Photo     string            `json:"photo"`
	Bio       string            `json:"bio"`
	Socials   map[string]string `json:"socials"`
	SortOrder int               `json:"sortOrder"`
}

func (a *API) listTeam(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListTeam(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) listAdminTeam(w http.ResponseWriter, r *http.Request) {
	a.listTeam(w, r)
}

func (a *API) getAdminTeamMember(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetTeamMember(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "team member not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) createTeamMember(w http.ResponseWriter, r *http.Request) {
	var in teamInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	item := &store.TeamMember{
		Name: in.Name, Role: in.Role, Photo: in.Photo, Bio: in.Bio,
		Socials: in.Socials, SortOrder: in.SortOrder,
	}
	if err := a.store.CreateTeamMember(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) updateTeamMember(w http.ResponseWriter, r *http.Request) {
	existing, err := a.store.GetTeamMember(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "team member not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in teamInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	existing.Name = in.Name
	existing.Role = in.Role
	existing.Photo = in.Photo
	existing.Bio = in.Bio
	existing.Socials = in.Socials
	existing.SortOrder = in.SortOrder
	if err := a.store.UpdateTeamMember(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (a *API) deleteTeamMember(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteTeamMember(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "team member not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
