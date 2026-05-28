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

type membershipPlan struct {
	Key              string  `json:"key"`
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	PriceUSD         float64 `json:"priceUSD"`
	PriceKESCents    int64   `json:"priceKESCents"`
	IncludesCourses  bool    `json:"includesCourses"`
	IncludesLibrary  bool    `json:"includesLibrary"`
	OrderProductName string  `json:"-"`
}

var membershipPlanCatalog = []membershipPlan{
	{
		Key:              "full",
		Name:             "Full membership",
		Description:      "Courses plus the members-only resource library.",
		PriceUSD:         10.00,
		IncludesCourses:  true,
		IncludesLibrary:  true,
		OrderProductName: "Kuza Kizazi Full Membership (1 month)",
	},
	{
		Key:              "library",
		Name:             "Library membership",
		Description:      "Resource library access only. Courses are not included.",
		PriceUSD:         1.90,
		IncludesCourses:  false,
		IncludesLibrary:  true,
		OrderProductName: "Kuza Kizazi Library Membership (1 month)",
	},
}

func (a *API) membershipPlans() []membershipPlan {
	plans := make([]membershipPlan, len(membershipPlanCatalog))
	copy(plans, membershipPlanCatalog)
	for i := range plans {
		plans[i].PriceKESCents = a.membershipKESCents(plans[i].PriceUSD)
	}
	return plans
}

func (a *API) membershipPlan(key string) (membershipPlan, bool) {
	if key == "" {
		key = "full"
	}
	for _, p := range a.membershipPlans() {
		if p.Key == key {
			return p, true
		}
	}
	return membershipPlan{}, false
}

// membershipKESCents converts a USD-denominated membership price into KES
// cents using the configured FX rate.
func (a *API) membershipKESCents(priceUSD float64) int64 {
	return int64(math.Round(priceUSD * a.cfg.KESPerUSD * 100))
}

// getMyMembership returns the signed-in user's membership state. Always 200
// — `status: "none"` means "never subscribed" and the frontend should show
// the join-now CTA.
func (a *API) getMyMembership(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	m, err := a.store.GetMembership(r.Context(), uid)
	if errors.Is(err, store.ErrNotFound) {
		plan, _ := a.membershipPlan("full")
		writeJSON(w, http.StatusOK, map[string]any{
			"status":        "none",
			"plan":          "",
			"plans":         a.membershipPlans(),
			"priceUSD":      plan.PriceUSD,
			"priceKESCents": plan.PriceKESCents,
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plan, ok := a.membershipPlan(m.Plan)
	if !ok {
		plan, _ = a.membershipPlan("full")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           m.Status,
		"plan":             m.Plan,
		"plans":            a.membershipPlans(),
		"currentPeriodEnd": m.CurrentPeriodEnd,
		"startedAt":        m.StartedAt,
		"cancelledAt":      m.CancelledAt,
		"isActive":         m.Status == "active" && m.CurrentPeriodEnd.After(time.Now().UTC()),
		"hasCourseAccess":  m.Status == "active" && m.Plan == "full" && m.CurrentPeriodEnd.After(time.Now().UTC()),
		"hasLibraryAccess": m.Status == "active" && m.CurrentPeriodEnd.After(time.Now().UTC()),
		"priceUSD":         plan.PriceUSD,
		"priceKESCents":    plan.PriceKESCents,
	})
}

// createMembershipCheckout creates a `kind=membership` order for the signed-in
// user. The client then drives the existing `/api/orders/{id}/pay` flow.
// Optional couponCode + applyCreditCents reduce the amount due.
func (a *API) createMembershipCheckout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CouponCode       string `json:"couponCode"`
		ApplyCreditCents int64  `json:"applyCreditCents"`
		Plan             string `json:"plan"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in) // body is optional
	uid := currentUserID(r)
	user, err := a.store.GetUserByID(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}

	plan, ok := a.membershipPlan(in.Plan)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown membership plan")
		return
	}
	if plan.Key == "library" {
		if existing, err := a.store.GetMembership(r.Context(), uid); err == nil &&
			existing.Status == "active" &&
			existing.Plan == "full" &&
			existing.CurrentPeriodEnd.After(time.Now().UTC()) {
			writeError(w, http.StatusConflict, "your full membership already includes the library-only plan")
			return
		}
	}
	subtotal := plan.PriceKESCents
	order := &store.Order{
		UserID:         &uid,
		Kind:           "membership",
		CustomerName:   user.Name,
		CustomerEmail:  user.Email,
		SubtotalCents:  subtotal,
		TotalCents:     subtotal,
		MembershipPlan: plan.Key,
	}
	items := []store.OrderItem{{
		ProductName:    plan.OrderProductName,
		UnitPriceCents: subtotal,
		Quantity:       1,
	}}
	if err := a.store.CreateOrderWithReservation(r.Context(), order, items, in.CouponCode, in.ApplyCreditCents, "memberships"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
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
	if active, _ := a.store.IsActiveCourseMember(r.Context(), uid); active {
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
	items := []store.OrderItem{{
		CourseID:       &cid,
		ProductName:    course.Title,
		UnitPriceCents: course.PriceCents,
		Quantity:       1,
	}}
	if err := a.store.CreateOrderWithReservation(r.Context(), order, items, in.CouponCode, in.ApplyCreditCents, "courses"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, order)
}
