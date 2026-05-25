package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

// listAdminTickets returns every customer ticket for the staff inbox.
func (a *API) listAdminTickets(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListAdminTickets(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// getAdminTicket returns one ticket + its message thread for staff.
func (a *API) getAdminTicket(w http.ResponseWriter, r *http.Request) {
	t, err := a.store.GetTicket(r.Context(), parseID(chi.URLParam(r, "id")), 0)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "ticket not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	msgs, _ := a.store.ListTicketMessages(r.Context(), t.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":   t,
		"messages": msgs,
	})
}

// replyToAdminTicket posts an admin reply on the thread.
func (a *API) replyToAdminTicket(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, err := a.store.GetUserByID(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}
	id := parseID(chi.URLParam(r, "id"))
	if _, err := a.store.GetTicket(r.Context(), id, 0); err != nil {
		writeError(w, http.StatusNotFound, "ticket not found")
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "reply body is required")
		return
	}
	authorID := uid
	m := &store.TicketMessage{
		TicketID:   id,
		AuthorID:   &authorID,
		AuthorRole: "admin",
		AuthorName: user.Name,
		Body:       body,
	}
	if err := a.store.AddTicketMessage(r.Context(), m); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// updateAdminTicket flips status (close / reopen).
func (a *API) updateAdminTicket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch in.Status {
	case "open", "replied", "closed":
	default:
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	err := a.store.SetTicketStatus(r.Context(), parseID(chi.URLParam(r, "id")), in.Status)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "ticket not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": in.Status})
}
