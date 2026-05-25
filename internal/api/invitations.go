package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"kuzakizazi/internal/store"
)

const invitationTTL = 7 * 24 * time.Hour

// listAdminInvitations returns every invitation (pending + accepted).
func (a *API) listAdminInvitations(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListInvitations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Decorate each invite with its share URL so the admin can copy it
	// even if SMTP isn't wired up yet.
	resp := make([]map[string]any, 0, len(items))
	for _, inv := range items {
		resp = append(resp, decorateInvite(a.cfg.PublicBaseURL, inv))
	}
	writeJSON(w, http.StatusOK, resp)
}

// createAdminInvitation issues a new invite and (best-effort) emails it.
func (a *API) createAdminInvitation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email  string `json:"email"`
		Name   string `json:"name"`
		RoleID int64  `json:"roleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	if !strings.Contains(in.Email, "@") {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if in.RoleID == 0 {
		writeError(w, http.StatusBadRequest, "roleId is required")
		return
	}
	role, err := a.store.GetRoleByID(r.Context(), in.RoleID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "role not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Block re-inviting an email that already has an active account.
	if existing, err := a.store.GetUserByEmail(r.Context(), in.Email); err == nil && existing != nil {
		writeError(w, http.StatusConflict, "an account with that email already exists")
		return
	}

	inviter := currentUserID(r)
	inv := &store.Invitation{
		Email:     in.Email,
		Name:      in.Name,
		RoleID:    role.ID,
		CreatedBy: &inviter,
	}
	if err := a.store.CreateInvitation(r.Context(), inv, invitationTTL); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	inv.RoleName = role.Name
	inv.RoleKey = role.Key

	inviteURL := buildInviteURL(a.cfg.PublicBaseURL, inv.Token)
	emailSent := false
	if a.mailer != nil {
		if err := a.mailer.SendInvite(r.Context(), inv.Email, inv.Name, role.Name, inviteURL); err == nil {
			emailSent = true
		}
	}

	resp := decorateInvite(a.cfg.PublicBaseURL, *inv)
	resp["emailSent"] = emailSent
	writeJSON(w, http.StatusCreated, resp)
}

func (a *API) deleteAdminInvitation(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteInvitation(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getInvitationPublic powers the /invite/{token} acceptance page. Returns
// only the fields the invitee needs to confirm what they're accepting.
func (a *API) getInvitationPublic(w http.ResponseWriter, r *http.Request) {
	inv, err := a.store.GetInvitationByToken(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if inv.AcceptedAt != nil {
		writeError(w, http.StatusGone, "this invitation has already been used")
		return
	}
	if time.Now().UTC().After(inv.ExpiresAt) {
		writeError(w, http.StatusGone, "this invitation has expired")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"email":     inv.Email,
		"name":      inv.Name,
		"roleName":  inv.RoleName,
		"roleKey":   inv.RoleKey,
		"expiresAt": inv.ExpiresAt,
	})
}

// acceptInvitation creates the user account from a pending invite,
// returns a JWT so the new user is immediately signed in.
func (a *API) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(in.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	inv, err := a.store.GetInvitationByToken(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if inv.AcceptedAt != nil {
		writeError(w, http.StatusGone, "this invitation has already been used")
		return
	}
	if time.Now().UTC().After(inv.ExpiresAt) {
		writeError(w, http.StatusGone, "this invitation has expired")
		return
	}

	// Belt-and-braces: check that no user has snuck in with the same email.
	if existing, err := a.store.GetUserByEmail(r.Context(), inv.Email); err == nil && existing != nil {
		writeError(w, http.StatusConflict, "an account with that email already exists")
		return
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = inv.Name
	}
	if name == "" {
		name = inv.Email
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}
	roleID := inv.RoleID
	user := &store.User{
		Email:        inv.Email,
		PasswordHash: string(hash),
		Name:         name,
		Role:         inv.RoleKey,
		RoleID:       &roleID,
	}
	if err := a.store.CreateUser(r.Context(), user); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.store.MarkInvitationAccepted(r.Context(), inv.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	token, err := a.issueToken(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "user": user})
}

func buildInviteURL(base, token string) string {
	base = strings.TrimRight(base, "/")
	return base + "/invite/" + token
}

func decorateInvite(base string, inv store.Invitation) map[string]any {
	status := "pending"
	if inv.AcceptedAt != nil {
		status = "accepted"
	} else if time.Now().UTC().After(inv.ExpiresAt) {
		status = "expired"
	}
	return map[string]any{
		"id":         inv.ID,
		"email":      inv.Email,
		"name":       inv.Name,
		"roleId":     inv.RoleID,
		"roleName":   inv.RoleName,
		"roleKey":    inv.RoleKey,
		"token":      inv.Token,
		"inviteUrl":  buildInviteURL(base, inv.Token),
		"expiresAt":  inv.ExpiresAt,
		"acceptedAt": inv.AcceptedAt,
		"createdAt":  inv.CreatedAt,
		"status":     status,
	}
}
