package api

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"kuzakizazi/internal/store"
)

// downloadClaims is what we sign into a download token. UID is the
// buyer, OID is the order they bought it on, DID is the specific file
// (product_downloads row). All three are needed so the server can
// verify ownership + enforce per-customer counts.
type downloadClaims struct {
	UID int64 `json:"uid"`
	OID int64 `json:"oid"`
	DID int64 `json:"did"`
	jwt.RegisteredClaims
}

// downloadTokenTTL is how long a freshly-minted download URL stays
// valid. Long enough for an email to be read and clicked, short enough
// that leaked URLs expire fast.
const downloadTokenTTL = 7 * 24 * time.Hour

// signDownloadToken mints a short-lived JWT that authorises one
// specific (user, order, file) download attempt. The token is the
// only piece of state the /api/downloads endpoint relies on.
func (a *API) signDownloadToken(userID, orderID, downloadID int64) (string, error) {
	claims := downloadClaims{
		UID: userID, OID: orderID, DID: downloadID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(downloadTokenTTL)),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(a.cfg.JWTSecret))
}

// downloadFile verifies the token, increments the per-customer
// counter (rolls back if the cap is hit), and streams the file with
// an attachment Content-Disposition so the browser saves it.
func (a *API) downloadFile(w http.ResponseWriter, r *http.Request) {
	tokenStr := chi.URLParam(r, "token")
	if tokenStr == "" {
		writeError(w, http.StatusBadRequest, "missing token")
		return
	}

	var claims downloadClaims
	token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(a.cfg.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		writeError(w, http.StatusUnauthorized, "invalid or expired download link")
		return
	}

	d, err := a.store.ConsumeDownload(r.Context(), claims.UID, claims.OID, claims.DID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "this download is not available for your account")
		return
	}
	if errors.Is(err, store.ErrDownloadLimit) {
		writeError(w, http.StatusTooManyRequests, "you have reached the download limit for this file")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// d.URL is the upload path, e.g. "/uploads/20260526-123456-abcd.pdf".
	// Resolve to a filesystem path under cfg.UploadDir.
	rel := strings.TrimPrefix(d.URL, "/uploads/")
	// Defence in depth: refuse any path that escapes the uploads dir.
	if strings.Contains(rel, "..") || strings.ContainsAny(rel, "\x00") || rel == "" {
		writeError(w, http.StatusBadRequest, "invalid download path")
		return
	}
	fullPath := filepath.Join(a.cfg.UploadDir, rel)

	f, err := os.Open(fullPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "the file is no longer available")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read file")
		return
	}

	// Best-effort mime type from the filename; default to octet-stream.
	mimeType := mime.TypeByExtension(filepath.Ext(rel))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	// Suggested filename for the Save As dialog — fall back to the
	// random storage name if the admin didn't label the file.
	suggested := d.Label
	if suggested == "" {
		suggested = rel
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.Header().Set("Content-Disposition", `attachment; filename="`+sanitiseFilename(suggested)+`"`)
	http.ServeContent(w, r, suggested, stat.ModTime(), f)
}

// sanitiseFilename strips characters that would break a Content-Disposition
// header. Keeps the implementation simple: replace quotes and control
// chars with underscores.
func sanitiseFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == '"' || r == '\\' {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
