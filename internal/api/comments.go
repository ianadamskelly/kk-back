package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

const maxCommentBytes = 2000

// listComments returns the comments for a published post.
func (a *API) listComments(w http.ResponseWriter, r *http.Request) {
	post, err := a.store.GetPostBySlug(r.Context(), chi.URLParam(r, "slug"), true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	comments, err := a.store.ListComments(r.Context(), post.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, comments)
}

// createComment adds a reader comment to a published post.
func (a *API) createComment(w http.ResponseWriter, r *http.Request) {
	post, err := a.store.GetPostBySlug(r.Context(), chi.URLParam(r, "slug"), true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var in struct {
		AuthorName string `json:"authorName"`
		Body       string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.AuthorName = strings.TrimSpace(in.AuthorName)
	in.Body = strings.TrimSpace(in.Body)
	if in.AuthorName == "" || in.Body == "" {
		writeError(w, http.StatusBadRequest, "name and comment are required")
		return
	}
	if len(in.AuthorName) > 80 {
		writeError(w, http.StatusBadRequest, "name is too long (max 80 characters)")
		return
	}
	if len(in.Body) > maxCommentBytes {
		writeError(w, http.StatusBadRequest, "comment is too long (max 2000 characters)")
		return
	}

	comment := &store.Comment{
		PostID:     post.ID,
		AuthorName: in.AuthorName,
		Body:       in.Body,
	}
	if err := a.store.CreateComment(r.Context(), comment); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

// listAdminComments returns every comment across all posts for moderation.
func (a *API) listAdminComments(w http.ResponseWriter, r *http.Request) {
	comments, err := a.store.ListAllComments(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, comments)
}

// deleteComment removes a comment.
func (a *API) deleteComment(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteComment(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
