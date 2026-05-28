package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

type libraryInput struct {
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Category    string `json:"category"`
	URL         string `json:"url"`
	Image       string `json:"image"`
	Status      string `json:"status"`
	SortOrder   int    `json:"sortOrder"`
}

// listPublicLibrary returns the published resource catalogue. The list
// is visible to everyone (so non-members can see what's inside and what
// they'd unlock), but each resource's URL is redacted unless the
// requester is an admin or an active member. Returns:
//
//	{ "entitled": bool, "resources": [...] }
func (a *API) listPublicLibrary(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListLibrary(r.Context(), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entitled := a.libraryEntitled(r)
	if !entitled {
		// Hide the actual download/destination URL. Titles, descriptions,
		// images, types, and categories stay visible — that's the carrot.
		for i := range items {
			items[i].URL = ""
		}
	} else {
		// Tokenise protected-file URLs so the browser can fetch them
		// through /api/files/{token} without an Authorization header.
		// External URLs are passed through unchanged.
		uid := int64(0)
		if claims := a.optionalClaims(r); claims != nil {
			uid = parseClaimsUserID(claims)
		}
		for i := range items {
			items[i].URL = a.signedFileURL(uid, items[i].URL)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entitled":  entitled,
		"resources": items,
	})
}

func (a *API) getPublicLibraryResource(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetLibraryResourceBySlug(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entitled := a.libraryEntitled(r)
	if !entitled {
		item.URL = ""
	} else {
		uid := int64(0)
		if claims := a.optionalClaims(r); claims != nil {
			uid = parseClaimsUserID(claims)
		}
		item.URL = a.signedFileURL(uid, item.URL)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entitled": entitled,
		"resource": item,
	})
}

// libraryEntitled tells whether the requester may see the unlocked
// library. Admins always do; logged-in users do if they hold an active
// membership.
func (a *API) libraryEntitled(r *http.Request) bool {
	claims := a.optionalClaims(r)
	if claims == nil {
		return false
	}
	if claims.Role == "admin" {
		return true
	}
	uid := parseClaimsUserID(claims)
	if uid == 0 {
		return false
	}
	active, _ := a.store.IsActiveMember(r.Context(), uid)
	return active
}

func (a *API) listAdminLibrary(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListLibrary(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) getAdminLibraryResource(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetLibraryResource(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) createLibraryResource(w http.ResponseWriter, r *http.Request) {
	var in libraryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	item := &store.LibraryResource{
		Title: in.Title, Slug: in.Slug, Description: sanitizeHTML(in.Description), Type: in.Type,
		Category: in.Category, URL: in.URL, Image: in.Image, Status: in.Status,
		SortOrder: in.SortOrder,
	}
	if err := a.store.CreateLibraryResource(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) updateLibraryResource(w http.ResponseWriter, r *http.Request) {
	existing, err := a.store.GetLibraryResource(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in libraryInput
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
	existing.Description = sanitizeHTML(in.Description)
	existing.Type = in.Type
	existing.Category = in.Category
	existing.URL = in.URL
	existing.Image = in.Image
	existing.Status = in.Status
	existing.SortOrder = in.SortOrder
	if err := a.store.UpdateLibraryResource(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (a *API) deleteLibraryResource(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteLibraryResource(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
