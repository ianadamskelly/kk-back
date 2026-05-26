package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

type productInput struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Description  string `json:"description"`
	Body         string `json:"body"`
	PriceCents   int64  `json:"priceCents"`
	Image        string `json:"image"`
	Category     string `json:"category"`
	Status       string `json:"status"`
	SortOrder    int    `json:"sortOrder"`
	Kind         string `json:"kind"`
	MaxDownloads *int   `json:"maxDownloads"`
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
	images, err := a.store.ListProductImages(r.Context(), item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	item.Images = images
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
	images, err := a.store.ListProductImages(r.Context(), item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	item.Images = images
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
	kind := in.Kind
	if kind != "digital" {
		kind = "physical"
	}
	item := &store.Product{
		Name: in.Name, Slug: in.Slug,
		Description: sanitizeHTML(in.Description),
		Body:        sanitizeHTML(in.Body),
		PriceCents:  in.PriceCents, Image: in.Image, Category: in.Category,
		Status: in.Status, SortOrder: in.SortOrder,
		Kind: kind, MaxDownloads: in.MaxDownloads,
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
	existing.Description = sanitizeHTML(in.Description)
	existing.Body = sanitizeHTML(in.Body)
	existing.PriceCents = in.PriceCents
	existing.Image = in.Image
	existing.Category = in.Category
	existing.Status = in.Status
	existing.SortOrder = in.SortOrder
	if in.Kind == "digital" {
		existing.Kind = "digital"
	} else {
		existing.Kind = "physical"
	}
	existing.MaxDownloads = in.MaxDownloads
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

// --- Product image gallery ---

func (a *API) listProductImages(w http.ResponseWriter, r *http.Request) {
	productID := parseID(chi.URLParam(r, "id"))
	images, err := a.store.ListProductImages(r.Context(), productID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, images)
}

// addProductImage attaches an already-uploaded image URL (returned by
// the existing POST /api/admin/upload endpoint) to a product.
func (a *API) addProductImage(w http.ResponseWriter, r *http.Request) {
	productID := parseID(chi.URLParam(r, "id"))
	if _, err := a.store.GetProductByID(r.Context(), productID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "product not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	img, err := a.store.AddProductImage(r.Context(), productID, in.URL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, img)
}

// reorderProductImages rewrites the position field on each image to
// match the given ordering. Body: {"ids": [3, 1, 2]}.
func (a *API) reorderProductImages(w http.ResponseWriter, r *http.Request) {
	productID := parseID(chi.URLParam(r, "id"))
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := a.store.SetProductImageOrder(r.Context(), productID, in.IDs); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "one of the images does not belong to this product")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) setProductCoverImage(w http.ResponseWriter, r *http.Request) {
	productID := parseID(chi.URLParam(r, "id"))
	imageID := parseID(chi.URLParam(r, "imageId"))
	if err := a.store.SetProductCoverImage(r.Context(), productID, imageID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "image not found for this product")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteProductImage(w http.ResponseWriter, r *http.Request) {
	productID := parseID(chi.URLParam(r, "id"))
	imageID := parseID(chi.URLParam(r, "imageId"))
	if err := a.store.DeleteProductImage(r.Context(), productID, imageID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "image not found for this product")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Digital downloads (admin-only) ---
// The URLs returned here are raw upload paths and must never be exposed
// on a public endpoint. Customer access flows through signed download
// tokens (see /api/downloads in a later phase).

func (a *API) listProductDownloads(w http.ResponseWriter, r *http.Request) {
	productID := parseID(chi.URLParam(r, "id"))
	items, err := a.store.ListProductDownloads(r.Context(), productID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) addProductDownload(w http.ResponseWriter, r *http.Request) {
	productID := parseID(chi.URLParam(r, "id"))
	if _, err := a.store.GetProductByID(r.Context(), productID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "product not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in struct {
		URL       string `json:"url"`
		Label     string `json:"label"`
		SizeBytes int64  `json:"sizeBytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	d := &store.ProductDownload{
		ProductID: productID,
		URL:       in.URL,
		Label:     in.Label,
		SizeBytes: in.SizeBytes,
	}
	if err := a.store.AddProductDownload(r.Context(), d); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (a *API) reorderProductDownloads(w http.ResponseWriter, r *http.Request) {
	productID := parseID(chi.URLParam(r, "id"))
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := a.store.SetProductDownloadOrder(r.Context(), productID, in.IDs); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "one of the files does not belong to this product")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteProductDownload(w http.ResponseWriter, r *http.Request) {
	productID := parseID(chi.URLParam(r, "id"))
	downloadID := parseID(chi.URLParam(r, "downloadId"))
	if err := a.store.DeleteProductDownload(r.Context(), productID, downloadID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "file not found for this product")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
