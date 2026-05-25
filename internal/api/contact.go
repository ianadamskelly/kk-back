package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

const maxMessageBytes = 5000

// createSubmission stores a message sent through the public contact form.
func (a *API) createSubmission(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		Email   string `json:"email"`
		Phone   string `json:"phone"`
		Service string `json:"service"`
		Subject string `json:"subject"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.TrimSpace(in.Email)
	in.Message = strings.TrimSpace(in.Message)
	if in.Name == "" || in.Email == "" || in.Message == "" {
		writeError(w, http.StatusBadRequest, "name, email, and message are required")
		return
	}
	if !strings.Contains(in.Email, "@") {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if len(in.Message) > maxMessageBytes {
		writeError(w, http.StatusBadRequest, "message is too long")
		return
	}

	sub := &store.ContactSubmission{
		Name:    in.Name,
		Email:   in.Email,
		Phone:   strings.TrimSpace(in.Phone),
		Service: strings.TrimSpace(in.Service),
		Subject: strings.TrimSpace(in.Subject),
		Message: in.Message,
	}
	if err := a.store.CreateSubmission(r.Context(), sub); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"message": "Thanks for reaching out — we'll be in touch soon.",
	})
}

// listSubmissions returns every contact submission for the admin inbox.
func (a *API) listSubmissions(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListSubmissions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// updateSubmission changes the triage status of a submission.
func (a *API) updateSubmission(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch in.Status {
	case "new", "read", "archived":
	default:
		writeError(w, http.StatusBadRequest, "status must be new, read, or archived")
		return
	}
	err := a.store.UpdateSubmissionStatus(r.Context(), parseID(chi.URLParam(r, "id")), in.Status)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "submission not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": in.Status})
}

// deleteSubmission removes a submission.
func (a *API) deleteSubmission(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteSubmission(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "submission not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
