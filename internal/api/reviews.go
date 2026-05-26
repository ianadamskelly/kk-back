package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

// listPublicProductReviews returns the published reviews + summary for
// the product identified by slug. Useful for the product detail page.
func (a *API) listPublicProductReviews(w http.ResponseWriter, r *http.Request) {
	product, err := a.store.GetProductBySlug(r.Context(), chi.URLParam(r, "slug"), true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeEntityReviews(w, r, "product", product.ID)
}

// listPublicCourseReviews — same shape, for courses.
func (a *API) listPublicCourseReviews(w http.ResponseWriter, r *http.Request) {
	course, err := a.store.GetCourseBySlug(r.Context(), chi.URLParam(r, "slug"), true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "course not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeEntityReviews(w, r, "course", course.ID)
}

// writeEntityReviews assembles the public response: summary + list +
// (for signed-in users) the caller's own review if they have one,
// regardless of status, so the form can show "your review is pending".
func (a *API) writeEntityReviews(w http.ResponseWriter, r *http.Request, entityType string, entityID int64) {
	summary, err := a.store.ReviewSummaryForEntity(r.Context(), entityType, entityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	reviews, err := a.store.ListReviewsForEntity(r.Context(), entityType, entityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := map[string]any{
		"summary": summary,
		"reviews": reviews,
	}

	// If the caller is signed in, also tell them whether they're allowed
	// to review + what they've already written (if anything). Saves an
	// extra round-trip from the frontend.
	if claims := a.optionalClaims(r); claims != nil {
		uid := parseClaimsUserID(claims)
		if uid > 0 {
			eligible, _ := a.userCanReview(r.Context(), uid, entityType, entityID)
			resp["canReview"] = eligible
			if mine, err := a.store.GetUserReview(r.Context(), uid, entityType, entityID); err == nil && mine != nil {
				resp["mine"] = mine
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// userCanReview enforces verified-buyer rules: a product review needs
// a confirmed/fulfilled order containing it; a course review needs
// either a direct purchase or an active membership.
func (a *API) userCanReview(ctx context.Context, userID int64, entityType string, entityID int64) (bool, error) {
	switch entityType {
	case "product":
		return a.store.HasUserPurchasedProduct(ctx, userID, entityID)
	case "course":
		return a.store.HasUserEnrolledInCourse(ctx, userID, entityID)
	default:
		return false, nil
	}
}

// upsertOwnReview creates or replaces the current user's review for
// an entity. Always lands in 'pending' so an admin can re-moderate
// after meaningful edits.
func (a *API) upsertOwnReview(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	var in struct {
		EntityType string `json:"entityType"`
		EntityID   int64  `json:"entityId"`
		Rating     int    `json:"rating"`
		Body       string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.EntityType = strings.ToLower(strings.TrimSpace(in.EntityType))
	if in.EntityType != "product" && in.EntityType != "course" {
		writeError(w, http.StatusBadRequest, "entityType must be 'product' or 'course'")
		return
	}
	if in.EntityID <= 0 {
		writeError(w, http.StatusBadRequest, "entityId is required")
		return
	}
	if in.Rating < 1 || in.Rating > 5 {
		writeError(w, http.StatusBadRequest, "rating must be between 1 and 5")
		return
	}

	eligible, err := a.userCanReview(r.Context(), uid, in.EntityType, in.EntityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !eligible {
		writeError(w, http.StatusForbidden, "you can only review purchases you've made")
		return
	}

	review := &store.Review{
		UserID:     uid,
		EntityType: in.EntityType,
		EntityID:   in.EntityID,
		Rating:     in.Rating,
		Body:       strings.TrimSpace(in.Body),
	}
	if err := a.store.UpsertReview(r.Context(), review); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, review)
}

// deleteOwnReview lets the customer delete their review.
func (a *API) deleteOwnReview(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id := parseID(chi.URLParam(r, "id"))
	if err := a.store.DeleteOwnReview(r.Context(), uid, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "review not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Admin moderation ---

func (a *API) listAdminReviews(w http.ResponseWriter, r *http.Request) {
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	items, err := a.store.AdminListReviews(r.Context(), status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) setAdminReviewStatus(w http.ResponseWriter, r *http.Request) {
	id := parseID(chi.URLParam(r, "id"))
	var in struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch in.Status {
	case "pending", "published", "rejected":
	default:
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if err := a.store.SetReviewStatus(r.Context(), id, in.Status); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "review not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": in.Status})
}

func (a *API) deleteAdminReview(w http.ResponseWriter, r *http.Request) {
	id := parseID(chi.URLParam(r, "id"))
	if err := a.store.AdminDeleteReview(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "review not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
