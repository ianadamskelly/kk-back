package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

func (a *API) listPublicPosts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := a.store.ListPosts(r.Context(), store.ListOptions{
		Search:        q.Get("q"),
		CategorySlug:  q.Get("category"),
		PublishedOnly: true,
		Page:          atoiDefault(q.Get("page"), 1),
		PerPage:       atoiDefault(q.Get("perPage"), 6),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) getPublicPost(w http.ResponseWriter, r *http.Request) {
	post, err := a.store.GetPostBySlug(r.Context(), chi.URLParam(r, "slug"), true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, post)
}

func (a *API) listAdminPosts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := a.store.ListPosts(r.Context(), store.ListOptions{
		Search:  q.Get("q"),
		Page:    atoiDefault(q.Get("page"), 1),
		PerPage: atoiDefault(q.Get("perPage"), 100),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) getAdminPost(w http.ResponseWriter, r *http.Request) {
	post, err := a.store.GetPostByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, post)
}

type postInput struct {
	Title       string     `json:"title"`
	Excerpt     string     `json:"excerpt"`
	Content     string     `json:"content"`
	CoverImage  string     `json:"coverImage"`
	Status      string     `json:"status"`
	ScheduledAt *time.Time `json:"scheduledAt"`
	CategoryID  *int64     `json:"categoryId"`
}

func writePostError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidPostSchedule) {
		writeError(w, http.StatusBadRequest, "scheduled posts require a future publish date")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func (a *API) createPost(w http.ResponseWriter, r *http.Request) {
	var in postInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	authorID := currentUserID(r)
	post := &store.Post{
		Title:       in.Title,
		Excerpt:     in.Excerpt,
		Content:     sanitizeHTML(in.Content),
		CoverImage:  in.CoverImage,
		Status:      in.Status,
		ScheduledAt: in.ScheduledAt,
		CategoryID:  in.CategoryID,
		AuthorID:    &authorID,
	}
	if err := a.store.CreatePost(r.Context(), post); err != nil {
		writePostError(w, err)
		return
	}
	full, _ := a.store.GetPostByID(r.Context(), post.ID)
	writeJSON(w, http.StatusCreated, full)
}

func (a *API) updatePost(w http.ResponseWriter, r *http.Request) {
	existing, err := a.store.GetPostByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var in postInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	existing.Title = in.Title
	existing.Excerpt = in.Excerpt
	existing.Content = sanitizeHTML(in.Content)
	existing.CoverImage = in.CoverImage
	existing.Status = strings.ToLower(strings.TrimSpace(in.Status))
	existing.ScheduledAt = in.ScheduledAt
	existing.CategoryID = in.CategoryID
	if err := a.store.UpdatePost(r.Context(), existing); err != nil {
		writePostError(w, err)
		return
	}
	full, _ := a.store.GetPostByID(r.Context(), existing.ID)
	writeJSON(w, http.StatusOK, full)
}

func (a *API) deletePost(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeletePost(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
