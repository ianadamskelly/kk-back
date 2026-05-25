package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

type productInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Body        string `json:"body"`
	PriceCents  int64  `json:"priceCents"`
	Image       string `json:"image"`
	Category    string `json:"category"`
	Status      string `json:"status"`
	SortOrder   int    `json:"sortOrder"`
}

func (a *API) listPublicProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items, err := a.store.ListProducts(r.Context(), store.ProductFilter{
		Search:        q.Get("q"),
		Category:      q.Get("category"),
		PublishedOnly: true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) getPublicProduct(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetProductBySlug(r.Context(), chi.URLParam(r, "slug"), true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) listAdminProducts(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListProducts(r.Context(), store.ProductFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) getAdminProduct(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetProductByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) createProduct(w http.ResponseWriter, r *http.Request) {
	var in productInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	item := &store.Product{
		Name: in.Name, Slug: in.Slug, Description: in.Description, Body: in.Body,
		PriceCents: in.PriceCents, Image: in.Image, Category: in.Category,
		Status: in.Status, SortOrder: in.SortOrder,
	}
	if err := a.store.CreateProduct(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) updateProduct(w http.ResponseWriter, r *http.Request) {
	existing, err := a.store.GetProductByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in productInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	existing.Name = in.Name
	existing.Slug = in.Slug
	existing.Description = in.Description
	existing.Body = in.Body
	existing.PriceCents = in.PriceCents
	existing.Image = in.Image
	existing.Category = in.Category
	existing.Status = in.Status
	existing.SortOrder = in.SortOrder
	if err := a.store.UpdateProduct(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (a *API) deleteProduct(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteProduct(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
