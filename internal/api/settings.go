package api

import (
	"encoding/json"
	"net/http"
)

// getSettings returns all site settings as a key/value object. It serves both
// the public site (contact info, social links) and the admin editor.
func (a *API) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := a.store.GetSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// updateSettings upserts the submitted key/value pairs.
func (a *API) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := a.store.UpdateSettings(r.Context(), in); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings, err := a.store.GetSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}
