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
)

// fileClaims signs a short-lived authorisation to read one protected
// file. UID is informational (logging only); the security boundary is
// the path encoded in the token, which is verified against
// ProtectedUploadDir before the file is streamed.
type fileClaims struct {
	UID  int64  `json:"uid,omitempty"`
	Path string `json:"p"`
	jwt.RegisteredClaims
}

// fileTokenTTL keeps an exposed link valid long enough for a reader
// to click through (and for an off-tab download to complete) but
// short enough that a leaked URL expires quickly.
const fileTokenTTL = 1 * time.Hour

// signFileToken mints a token that authorises one fetch of `relPath`
// from ProtectedUploadDir. relPath is the URL form ("/files/<name>")
// stored alongside library entries / submissions / product downloads.
func (a *API) signFileToken(uid int64, relPath string) (string, error) {
	claims := fileClaims{
		UID:  uid,
		Path: relPath,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(fileTokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(a.cfg.JWTSecret))
}

// signedFileURL returns the public path the frontend should hit to
// fetch the file behind `storedURL`. External URLs (http/https) pass
// through unchanged. Internal `/files/...` URLs get tokenised so the
// browser hits /api/files/{token} with no need for an Authorization
// header (handy for <a href> and <img src>).
func (a *API) signedFileURL(uid int64, storedURL string) string {
	if storedURL == "" {
		return ""
	}
	if strings.HasPrefix(storedURL, "http://") || strings.HasPrefix(storedURL, "https://") {
		return storedURL
	}
	if !strings.HasPrefix(storedURL, "/files/") {
		// Legacy /uploads/... lives in the public image dir; expose
		// it as-is so existing rows keep working. New uploads should
		// be /files/ already.
		return storedURL
	}
	token, err := a.signFileToken(uid, storedURL)
	if err != nil {
		return ""
	}
	return "/api/files/" + token
}

// servePublicFileToken is the only public way into the protected
// uploads dir. It validates the token, resolves the path safely under
// ProtectedUploadDir (rejecting any traversal), and streams the file
// back with a sensible mime type + Content-Disposition: inline so
// previewable formats (PDF, images) open in-tab.
func (a *API) servePublicFileToken(w http.ResponseWriter, r *http.Request) {
	tokenStr := chi.URLParam(r, "token")
	if tokenStr == "" {
		writeError(w, http.StatusBadRequest, "missing token")
		return
	}
	var claims fileClaims
	tok, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(a.cfg.JWTSecret), nil
	})
	if err != nil || !tok.Valid {
		writeError(w, http.StatusUnauthorized, "invalid or expired file link")
		return
	}

	// claims.Path must start with /files/ and not escape the
	// protected dir. Anything else is a forged or malformed token.
	rel := strings.TrimPrefix(claims.Path, "/files/")
	if rel == claims.Path || rel == "" || strings.Contains(rel, "..") || strings.ContainsAny(rel, "\x00") {
		writeError(w, http.StatusBadRequest, "invalid file path in token")
		return
	}

	fullPath := filepath.Join(a.cfg.ProtectedUploadDir, rel)
	f, err := os.Open(fullPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file no longer available")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read file")
		return
	}

	mimeType := mime.TypeByExtension(filepath.Ext(rel))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	// Inline so PDFs / images preview in the browser. The signed
	// product download flow (/api/downloads/{token}) sets attachment
	// instead and uses the original filename — keep them separate.
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, rel, stat.ModTime(), f)
}
