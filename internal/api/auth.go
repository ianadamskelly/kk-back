package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"kuzakizazi/internal/store"
)

type ctxKey int

const (
	claimsKey ctxKey = iota
	permsKey
)

// sessionCookieName is the HttpOnly cookie that carries the customer
// JWT. Admin sessions still use the Authorization header (no rich-text
// rendering risk and the admin tree is small). Customers used to
// keep this token in localStorage, which XSS could read; the cookie
// is HttpOnly so JS never sees it.
const sessionCookieName = "kk_session"

// setSessionCookie writes a fresh HttpOnly session cookie carrying
// the JWT. Same TTL as the token. Domain comes from config so it can
// span subdomains in prod (".kuzakizazi.com") and stay omitted in
// dev (browser scopes to localhost).
func (a *API) setSessionCookie(w http.ResponseWriter, token string) {
	c := &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(24 * time.Hour / time.Second),
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		// Lax lets the cookie ride along on top-level navigations
		// (so SSR pages can render with the user's data) but still
		// blocks cross-site POSTs — the standard SPA CSRF posture.
		SameSite: http.SameSiteLaxMode,
	}
	if a.cfg.CookieDomain != "" {
		c.Domain = a.cfg.CookieDomain
	}
	http.SetCookie(w, c)
}

// clearSessionCookie deletes the cookie by writing one with MaxAge<0.
// Mirrors setSessionCookie's attributes so the browser actually
// matches it for deletion.
func (a *API) clearSessionCookie(w http.ResponseWriter) {
	c := &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
	if a.cfg.CookieDomain != "" {
		c.Domain = a.cfg.CookieDomain
	}
	http.SetCookie(w, c)
}

// logout clears the session cookie. Idempotent — safe to call
// without a session.
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	a.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// tokenClaims is the JWT payload: the user id (Subject) plus their role.
type tokenClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func (a *API) issueToken(u *store.User) (string, error) {
	claims := tokenClaims{
		Role: u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(u.ID, 10),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(a.cfg.JWTSecret))
}

// login validates account credentials and returns a JWT.
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))

	user, err := a.store.GetUserByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	token, err := a.issueToken(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	// Set the HttpOnly cookie for customer sessions so the SPA
	// doesn't have to keep the JWT in localStorage. Admins keep
	// reading the token from the response body since the admin app
	// still relies on Bearer headers (and the admin XSS surface is
	// smaller anyway). Setting both is harmless.
	if user.Role == "customer" {
		a.setSessionCookie(w, token)
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}

// parseClaims tries to verify a raw JWT string. Returns the decoded
// claims + true on success; nil + false on any failure (empty input,
// malformed token, wrong signature, expired). Pure helper so the
// caller can try multiple credential sources without duplication.
func (a *API) parseClaims(raw string) (*tokenClaims, bool) {
	if raw == "" {
		return nil, false
	}
	var claims tokenClaims
	tok, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(a.cfg.JWTSecret), nil
	})
	if err != nil || !tok.Valid {
		return nil, false
	}
	return &claims, true
}

// claimsFromRequest tries the Authorization: Bearer header first,
// then the kk_session cookie. Either may be present (admin still
// sends headers, customers ride the cookie); a stale / empty header
// transparently falls through to the cookie so the frontend doesn't
// have to coordinate which credential to send.
func (a *API) claimsFromRequest(r *http.Request) *tokenClaims {
	if parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		if claims, ok := a.parseClaims(parts[1]); ok {
			return claims
		}
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if claims, ok := a.parseClaims(c.Value); ok {
			return claims
		}
	}
	return nil
}

// requireAuth rejects requests without a valid credential and stashes
// the decoded claims in the request context.
func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := a.claimsFromRequest(r)
		if claims == nil {
			writeError(w, http.StatusUnauthorized, "missing or invalid credentials")
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdmin rejects authenticated requests that lack the admin role.
// It must run after requireAuth.
func (a *API) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(claimsKey).(*tokenClaims)
		if !ok || claims.Role != "admin" {
			writeError(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loadPermissions fetches the user's permission set once per request and
// stashes it on the context so requirePermission/requireAnyPermission can
// check without re-querying. Must run after requireAuth.
func (a *API) loadPermissions(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := currentUserID(r)
		perms, err := a.store.UserPermissions(r.Context(), uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not load permissions")
			return
		}
		ctx := context.WithValue(r.Context(), permsKey, perms)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// hasPermission tells whether the loaded permission set contains the key.
func hasPermission(r *http.Request, key string) bool {
	perms, _ := r.Context().Value(permsKey).([]string)
	for _, p := range perms {
		if p == key {
			return true
		}
	}
	return false
}

// hasAnyPermission returns true if the user holds at least one of the keys.
// Used as the broad "is this a staff member" gate before per-route checks.
func hasAnyPermission(r *http.Request, keys ...string) bool {
	perms, _ := r.Context().Value(permsKey).([]string)
	if len(perms) == 0 {
		return false
	}
	set := map[string]struct{}{}
	for _, p := range perms {
		set[p] = struct{}{}
	}
	for _, k := range keys {
		if _, ok := set[k]; ok {
			return true
		}
	}
	return false
}

// requirePermission rejects requests whose user doesn't hold the given
// permission. Must run after requireAuth + loadPermissions.
func (a *API) requirePermission(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hasPermission(r, key) {
				writeError(w, http.StatusForbidden, "missing permission: "+key)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireAnyPermission lets the request through if the user holds ANY of
// the listed permissions. Used as the entry gate for the admin tree so a
// user with even one section open can hit the /admin/me + /admin/upload
// utility endpoints.
func (a *API) requireAnyPermission(keys ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hasAnyPermission(r, keys...) {
				writeError(w, http.StatusForbidden, "admin access required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// currentUserID returns the id of the authenticated user, or 0 if none.
func currentUserID(r *http.Request) int64 {
	claims, ok := r.Context().Value(claimsKey).(*tokenClaims)
	if !ok {
		return 0
	}
	id, _ := strconv.ParseInt(claims.Subject, 10, 64)
	return id
}

// me returns the signed-in account, used by the frontend to verify a token.
// Includes the user's role + permission list so the admin UI can hide menu
// items the user isn't allowed to use.
func (a *API) me(w http.ResponseWriter, r *http.Request) {
	user, err := a.store.GetUserByID(r.Context(), currentUserID(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}
	perms, _ := a.store.UserPermissions(r.Context(), user.ID)
	if perms == nil {
		perms = []string{}
	}
	resp := map[string]any{
		"id":          user.ID,
		"email":       user.Email,
		"name":        user.Name,
		"role":        user.Role,
		"roleId":      user.RoleID,
		"createdAt":   user.CreatedAt,
		"permissions": perms,
	}
	if user.RoleID != nil {
		if role, err := a.store.GetRoleByID(r.Context(), *user.RoleID); err == nil {
			resp["roleName"] = role.Name
			resp["roleKey"] = role.Key
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// register creates a customer account and returns a JWT. An optional
// referralCode in the body links the new user to a referrer so the
// referrer gets credit when the referee makes their first paid order.
// `source` (optional) tells us where the signup came from so we can tag
// the resulting newsletter subscriber for later targeting.
func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email        string `json:"email"`
		Password     string `json:"password"`
		Name         string `json:"name"`
		ReferralCode string `json:"referralCode"`
		Source       string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	name := strings.TrimSpace(body.Name)
	if !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if len(body.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if existing, err := a.store.GetUserByEmail(r.Context(), email); err == nil && existing != nil {
		writeError(w, http.StatusBadRequest, "an account with that email already exists")
		return
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}
	user := &store.User{
		Email: email, Name: name, Role: "customer", PasswordHash: string(hash),
	}
	if err := a.store.CreateUser(r.Context(), user); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Best-effort referral attribution. A bad/unknown code is silently
	// ignored — we don't want a typo'd ?ref= to block signup.
	if code := strings.TrimSpace(body.ReferralCode); code != "" {
		if referrer, err := a.store.GetUserByReferralCode(r.Context(), code); err == nil && referrer != nil && referrer.ID != user.ID {
			_ = a.store.SetReferrer(r.Context(), user.ID, referrer.ID)
		}
	}

	// Auto-add to the mailing list, tagged by source. Errors are
	// swallowed: a flaky subscriber upsert must never block account
	// creation.
	source := strings.ToLower(strings.TrimSpace(body.Source))
	if source == "" {
		source = "signup"
	}
	uid := user.ID
	_, _ = a.store.UpsertSubscriberWithTags(r.Context(), store.SubscriberUpsert{
		Email:  user.Email,
		Name:   user.Name,
		Source: source,
		UserID: &uid,
		Tags:   []string{"signup"},
	})

	// Fire-and-forget welcome email. SMTP failures are logged but never
	// surfaced to the user — their account is created either way.
	if a.mailer != nil {
		go func(email, name string) {
			if err := a.mailer.SendWelcome(context.Background(), email, name); err != nil {
				log.Printf("welcome email to %s failed: %v", email, err)
			}
		}(user.Email, user.Name)
	}

	token, err := a.issueToken(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	// Register is customer-only; always set the HttpOnly session cookie.
	a.setSessionCookie(w, token)
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "user": user})
}

// parseClaimsUserID returns the user id encoded in JWT claims, or 0.
func parseClaimsUserID(c *tokenClaims) int64 {
	if c == nil {
		return 0
	}
	id, _ := strconv.ParseInt(c.Subject, 10, 64)
	return id
}

// optionalClaims returns the JWT claims when a valid credential is
// present, or nil otherwise. Used on public endpoints that behave
// slightly differently for signed-in users.
func (a *API) optionalClaims(r *http.Request) *tokenClaims {
	return a.claimsFromRequest(r)
}
