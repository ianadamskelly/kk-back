package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

type testimonialInput struct {
	Author    string `json:"author"`
	Role      string `json:"role"`
	Company   string `json:"company"`
	Quote     string `json:"quote"`
	Avatar    string `json:"avatar"`
	SortOrder int    `json:"sortOrder"`
	Status    string `json:"status"`
}

func (a *API) listPublicTestimonials(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListTestimonials(r.Context(), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) listAdminTestimonials(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListTestimonials(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) getAdminTestimonial(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetTestimonial(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "testimonial not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) createTestimonial(w http.ResponseWriter, r *http.Request) {
	var in testimonialInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Author == "" || in.Quote == "" {
		writeError(w, http.StatusBadRequest, "author and quote are required")
		return
	}
	item := &store.Testimonial{
		Author: in.Author, Role: in.Role, Company: in.Company, Quote: in.Quote,
		Avatar: in.Avatar, SortOrder: in.SortOrder, Status: in.Status,
	}
	if err := a.store.CreateTestimonial(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) updateTestimonial(w http.ResponseWriter, r *http.Request) {
	existing, err := a.store.GetTestimonial(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "testimonial not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in testimonialInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Author == "" || in.Quote == "" {
		writeError(w, http.StatusBadRequest, "author and quote are required")
		return
	}
	existing.Author = in.Author
	existing.Role = in.Role
	existing.Company = in.Company
	existing.Quote = in.Quote
	existing.Avatar = in.Avatar
	existing.SortOrder = in.SortOrder
	existing.Status = in.Status
	if err := a.store.UpdateTestimonial(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (a *API) deleteTestimonial(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteTestimonial(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "testimonial not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
