package api

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"kuzakizazi/internal/store"
)

type assetSessionClaims struct {
	UID         int64  `json:"uid"`
	AssetSlug   string `json:"assetSlug"`
	Entitlement int64  `json:"entitlementId"`
	jwt.RegisteredClaims
}

const assetSessionTokenTTL = 10 * time.Minute

func (a *API) signAssetSessionToken(userID int64, assetSlug string, entitlementID int64) (string, time.Time, error) {
	expires := time.Now().Add(assetSessionTokenTTL)
	claims := assetSessionClaims{
		UID:         userID,
		AssetSlug:   assetSlug,
		Entitlement: entitlementID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(a.cfg.JWTSecret))
	return token, expires, err
}

func (a *API) listMyInteractiveAssets(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListInteractiveAssetEntitlements(r.Context(), currentUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) getMyInteractiveAsset(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	assetSlug := chi.URLParam(r, "assetId")
	if !store.IsKnownInteractiveAsset(assetSlug) {
		writeError(w, http.StatusNotFound, "asset not found")
		return
	}
	ent, err := a.store.GetInteractiveAssetEntitlement(r.Context(), uid, assetSlug)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusForbidden, "you do not have access to this asset")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	token, expires, err := a.signAssetSessionToken(uid, assetSlug, ent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	user, err := a.store.GetUserByID(r.Context(), uid)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}
	_ = a.store.LogInteractiveAssetEvent(r.Context(), assetEventFromRequest(r, ent, "open"))
	writeJSON(w, http.StatusOK, map[string]any{
		"entitlement":           ent,
		"assetSessionToken":     token,
		"assetSessionExpiresAt": expires,
		"watermark": map[string]string{
			"email":     user.Email,
			"licenseId": ent.LicenseID,
		},
	})
}

func (a *API) exportMyInteractiveAsset(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	assetSlug := chi.URLParam(r, "assetId")
	if !store.IsKnownInteractiveAsset(assetSlug) {
		writeError(w, http.StatusNotFound, "asset not found")
		return
	}
	ent, err := a.store.ConsumeInteractiveAssetExport(r.Context(), uid, assetSlug)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusForbidden, "you do not have access to this asset")
		return
	}
	if errors.Is(err, store.ErrAssetUseLimit) {
		writeError(w, http.StatusTooManyRequests, "you have reached the export limit for this worksheet")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	user, err := a.store.GetUserByID(r.Context(), uid)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}
	_ = a.store.LogInteractiveAssetEvent(r.Context(), assetEventFromRequest(r, ent, "export"))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"entitlement": ent,
		"watermark": map[string]string{
			"email":     user.Email,
			"licenseId": ent.LicenseID,
		},
	})
}

func assetEventFromRequest(r *http.Request, ent *store.InteractiveAssetEntitlement, eventType string) store.InteractiveAssetEvent {
	return store.InteractiveAssetEvent{
		EntitlementID: ent.ID,
		UserID:        ent.UserID,
		AssetSlug:     ent.AssetSlug,
		EventType:     eventType,
		IPAddress:     requestIP(r),
		UserAgent:     r.UserAgent(),
	}
}

func requestIP(r *http.Request) string {
	for _, h := range []string{"CF-Connecting-IP", "X-Real-IP", "X-Forwarded-For"} {
		v := strings.TrimSpace(r.Header.Get(h))
		if v == "" {
			continue
		}
		if h == "X-Forwarded-For" {
			v = strings.TrimSpace(strings.Split(v, ",")[0])
		}
		if net.ParseIP(v) != nil {
			return v
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
