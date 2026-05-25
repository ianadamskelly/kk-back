package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"kuzakizazi/internal/store"
)

// listAdminUsers returns staff users (anyone with a role_id) for the
// /admin/users page.
func (a *API) listAdminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.store.ListStaffUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// updateAdminUser reassigns a staff member's role. Guarded against
// demoting the very last admin.
func (a *API) updateAdminUser(w http.ResponseWriter, r *http.Request) {
	target, err := a.store.GetUserByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in struct {
		RoleID int64 `json:"roleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.RoleID == 0 {
		writeError(w, http.StatusBadRequest, "roleId is required")
		return
	}
	newRole, err := a.store.GetRoleByID(r.Context(), in.RoleID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "role not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Prevent demoting the last admin.
	if target.Role == "admin" && newRole.Key != "admin" {
		count, err := a.store.AdminUserCount(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if count <= 1 {
			writeError(w, http.StatusConflict, "at least one admin must remain")
			return
		}
	}

	if err := a.store.SetUserRole(r.Context(), target.ID, newRole.ID, newRole.Key); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       target.ID,
		"roleId":   newRole.ID,
		"roleKey":  newRole.Key,
		"roleName": newRole.Name,
	})
}

// deleteAdminUser removes a staff user; the last admin can't be deleted.
func (a *API) deleteAdminUser(w http.ResponseWriter, r *http.Request) {
	target, err := a.store.GetUserByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if target.ID == currentUserID(r) {
		writeError(w, http.StatusConflict, "you cannot delete your own account")
		return
	}
	if target.Role == "admin" {
		count, err := a.store.AdminUserCount(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if count <= 1 {
			writeError(w, http.StatusConflict, "at least one admin must remain")
			return
		}
	}
	if err := a.store.DeleteUser(r.Context(), target.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// changeOwnPassword lets a signed-in user rotate their password. Not gated
// by any permission; lives on the /auth side. (Wired in server.go.)
func (a *API) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(in.New) < 8 {
		writeError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	user, err := a.store.GetUserByID(r.Context(), currentUserID(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Current)) != nil {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.New), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}
	if err := a.store.SetUserPassword(r.Context(), user.ID, string(hash)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
