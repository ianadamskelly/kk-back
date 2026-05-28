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

func (a *API) adminRevenueSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := a.store.RevenueSummary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (a *API) listAdminMemberships(w http.ResponseWriter, r *http.Request) {
	if _, err := a.store.ExpireOverdueMemberships(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, err := a.store.ListMemberships(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) listAdminServiceRevenue(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListServiceRevenue(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) createAdminServiceRevenue(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ServiceID   *int64 `json:"serviceId"`
		ServiceName string `json:"serviceName"`
		ClientName  string `json:"clientName"`
		AmountCents int64  `json:"amountCents"`
		Currency    string `json:"currency"`
		OccurredAt  string `json:"occurredAt"`
		Note        string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.AmountCents <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be greater than zero")
		return
	}
	occurred := time.Now().UTC()
	if s := strings.TrimSpace(in.OccurredAt); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "occurredAt must be YYYY-MM-DD")
			return
		}
		occurred = t
	}
	row := &store.ServiceRevenue{
		ServiceID:   in.ServiceID,
		ServiceName: strings.TrimSpace(in.ServiceName),
		ClientName:  strings.TrimSpace(in.ClientName),
		AmountCents: in.AmountCents,
		Currency:    strings.ToUpper(strings.TrimSpace(in.Currency)),
		OccurredAt:  occurred,
		Note:        strings.TrimSpace(in.Note),
	}
	if err := a.store.CreateServiceRevenue(r.Context(), row); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

func (a *API) deleteAdminServiceRevenue(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteServiceRevenue(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "entry not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
