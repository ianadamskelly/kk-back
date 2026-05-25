package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

// listPermissions returns the permission catalog grouped by resource so the
// admin UI can render the matrix without hard-coding the list.
func (a *API) listPermissions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"all":       store.AllPermissions(),
		"resources": store.PermissionResources(),
	})
}

// listAdminRoles returns every role with its permissions and user counts.
func (a *API) listAdminRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := a.store.ListRoles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

func (a *API) getAdminRole(w http.ResponseWriter, r *http.Request) {
	role, err := a.store.GetRoleByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "role not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, role)
}

type roleInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

func (a *API) createAdminRole(w http.ResponseWriter, r *http.Request) {
	var in roleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	perms, err := sanitizePermissions(in.Permissions)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	role := &store.Role{
		Name:        strings.TrimSpace(in.Name),
		Description: strings.TrimSpace(in.Description),
		Permissions: perms,
	}
	if err := a.store.CreateRole(r.Context(), role); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, role)
}

func (a *API) updateAdminRole(w http.ResponseWriter, r *http.Request) {
	role, err := a.store.GetRoleByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "role not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if role.Key == "admin" {
		writeError(w, http.StatusForbidden, "the admin role cannot be edited")
		return
	}
	var in roleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	perms, err := sanitizePermissions(in.Permissions)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	role.Name = strings.TrimSpace(in.Name)
	role.Description = strings.TrimSpace(in.Description)
	role.Permissions = perms
	if err := a.store.UpdateRole(r.Context(), role); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, role)
}

func (a *API) deleteAdminRole(w http.ResponseWriter, r *http.Request) {
	role, err := a.store.GetRoleByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "role not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if role.IsBuiltin {
		writeError(w, http.StatusForbidden, "built-in roles cannot be deleted")
		return
	}
	if role.UserCount > 0 {
		writeError(w, http.StatusConflict, "this role is still assigned to users — reassign them first")
		return
	}
	if err := a.store.DeleteRole(r.Context(), role.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sanitizePermissions de-dupes and validates the requested permission keys
// against the known catalog so the role can never store garbage.
func sanitizePermissions(in []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !store.IsValidPermission(p) {
			return nil, errors.New("unknown permission: " + p)
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out, nil
}
