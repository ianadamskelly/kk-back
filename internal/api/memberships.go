package api

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

// membershipUSDPrice is the published membership price in USD. KES totals
// are derived from this using the configured KES_PER_USD rate so they stay
// in sync with the rest of the site's KES-denominated pricing.
const membershipUSDPrice = 10.00

// membershipKESCents converts the USD-denominated membership price into KES
// cents using the configured FX rate.
func (a *API) membershipKESCents() int64 {
	return int64(math.Round(membershipUSDPrice * a.cfg.KESPerUSD * 100))
}

// getMyMembership returns the signed-in user's membership state. Always 200
// — `status: "none"` means "never subscribed" and the frontend should show
// the join-now CTA.
func (a *API) getMyMembership(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	m, err := a.store.GetMembership(r.Context(), uid)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "none",
			"priceUSD":       membershipUSDPrice,
			"priceKESCents":  a.membershipKESCents(),
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":            m.Status,
		"currentPeriodEnd":  m.CurrentPeriodEnd,
		"startedAt":         m.StartedAt,
		"cancelledAt":       m.CancelledAt,
		"isActive":          m.Status == "active" && m.CurrentPeriodEnd.After(time.Now().UTC()),
		"priceUSD":          membershipUSDPrice,
		"priceKESCents":     a.membershipKESCents(),
	})
}

// createMembershipCheckout creates a `kind=membership` order for the signed-in
// user. The client then drives the existing `/api/orders/{id}/pay` flow.
// Optional couponCode + applyCreditCents reduce the amount due.
func (a *API) createMembershipCheckout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CouponCode       string `json:"couponCode"`
		ApplyCreditCents int64  `json:"applyCreditCents"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in) // body is optional
	uid := currentUserID(r)
	user, err := a.store.GetUserByID(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}

	subtotal := a.membershipKESCents()
	order := &store.Order{
		UserID:        &uid,
		Kind:          "membership",
		CustomerName:  user.Name,
		CustomerEmail: user.Email,
		SubtotalCents: subtotal,
		TotalCents:    subtotal,
	}
	if err := a.applyDiscountsAndCredit(r.Context(), order, &uid, in.CouponCode, in.ApplyCreditCents, "memberships"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items := []store.OrderItem{{
		ProductName:    "Kuza Kizazi Membership (1 month)",
		UnitPriceCents: subtotal,
		Quantity:       1,
	}}
	if err := a.store.CreateOrder(r.Context(), order, items); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Coupon + credit are consumed at payment-verify time, not here.
	// See orders.go for the rationale.
	writeJSON(w, http.StatusCreated, order)
}

// createCourseCheckout creates a `kind=course` order for the signed-in user
// to buy one specific course. Free courses (priceCents=0) are rejected — the
// frontend should just send the user straight into lessons.
func (a *API) createCourseCheckout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CouponCode       string `json:"couponCode"`
		ApplyCreditCents int64  `json:"applyCreditCents"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)

	uid := currentUserID(r)
	user, err := a.store.GetUserByID(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}

	course, err := a.store.GetCourseByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) || (err == nil && course.Status != "published") {
		writeError(w, http.StatusNotFound, "course not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if course.PriceCents <= 0 {
		writeError(w, http.StatusBadRequest, "this course is free")
		return
	}

	// Block re-purchase: if they already own it or are an active member, the
	// checkout would be a waste of money.
	if owned, _ := a.store.UserOwnsCourse(r.Context(), uid, course.ID); owned {
		writeError(w, http.StatusConflict, "you already own this course")
		return
	}
	if active, _ := a.store.IsActiveMember(r.Context(), uid); active {
		writeError(w, http.StatusConflict, "your membership already includes this course")
		return
	}

	cid := course.ID
	order := &store.Order{
		UserID:        &uid,
		Kind:          "course",
		CustomerName:  user.Name,
		CustomerEmail: user.Email,
		SubtotalCents: course.PriceCents,
		TotalCents:    course.PriceCents,
	}
	if err := a.applyDiscountsAndCredit(r.Context(), order, &uid, in.CouponCode, in.ApplyCreditCents, "courses"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items := []store.OrderItem{{
		CourseID:       &cid,
		ProductName:    course.Title,
		UnitPriceCents: course.PriceCents,
		Quantity:       1,
	}}
	if err := a.store.CreateOrder(r.Context(), order, items); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Coupon + credit are consumed at payment-verify time, not here.
	writeJSON(w, http.StatusCreated, order)
}
