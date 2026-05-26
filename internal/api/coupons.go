package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

// validatedCoupon is the result of running a code against an in-flight
// order. DiscountCents is what to subtract from the subtotal.
type validatedCoupon struct {
	Coupon        *store.Coupon
	DiscountCents int64
}

// validateCouponForOrder runs every check that gates a coupon: scope,
// active flag, start/expiry, usage caps, minimum subtotal. Returns a
// user-facing error message on the first failure.
func (a *API) validateCouponForOrder(ctx context.Context, code, scope string, subtotalCents int64, userID *int64) (*validatedCoupon, error) {
	c, err := a.store.GetCouponByCode(ctx, code)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("coupon not found")
	}
	if err != nil {
		return nil, err
	}
	if !c.Active {
		return nil, fmt.Errorf("this coupon is no longer active")
	}
	now := time.Now().UTC()
	if c.StartsAt != nil && now.Before(*c.StartsAt) {
		return nil, fmt.Errorf("this coupon isn't valid yet")
	}
	if c.ExpiresAt != nil && now.After(*c.ExpiresAt) {
		return nil, fmt.Errorf("this coupon has expired")
	}
	if c.Scope != "all" && c.Scope != scope {
		return nil, fmt.Errorf("this coupon doesn't apply to %s", scope)
	}
	if c.MaxUses != nil && c.UsedCount >= *c.MaxUses {
		return nil, fmt.Errorf("this coupon has been fully redeemed")
	}
	if c.MinSubtotalCents > 0 && subtotalCents < c.MinSubtotalCents {
		return nil, fmt.Errorf("subtotal must be at least %s to use this coupon",
			fmtKES(c.MinSubtotalCents))
	}
	if userID != nil && c.PerUserMaxUses != nil {
		n, err := a.store.CountCouponUsesByUser(ctx, c.ID, *userID)
		if err != nil {
			return nil, err
		}
		if n >= *c.PerUserMaxUses {
			return nil, fmt.Errorf("you've already used this coupon the maximum number of times")
		}
	}

	discount := computeDiscount(c, subtotalCents)
	if discount <= 0 {
		return nil, fmt.Errorf("this coupon doesn't reduce your total")
	}
	return &validatedCoupon{Coupon: c, DiscountCents: discount}, nil
}

// computeDiscount applies the coupon's rules. The result is capped at the
// subtotal so a coupon never produces negative totals.
func computeDiscount(c *store.Coupon, subtotal int64) int64 {
	var d int64
	switch c.DiscountType {
	case "percent":
		d = subtotal * int64(c.PercentOff) / 100
	case "amount":
		d = c.AmountOffCents
	}
	if d > subtotal {
		d = subtotal
	}
	if d < 0 {
		d = 0
	}
	return d
}

func fmtKES(cents int64) string {
	return fmt.Sprintf("KSh %d", cents/100)
}

// applyDiscountsAndCredit mutates the in-flight order to deduct a coupon
// + store credit. Called by every checkout entry point (shop, course,
// membership) so the rules apply uniformly. Caller must still persist the
// order, then call recordOrderDiscounts to write redemption + ledger rows.
func (a *API) applyDiscountsAndCredit(ctx context.Context, order *store.Order, userID *int64, couponCode string, applyCreditCents int64, scope string) error {
	if strings.TrimSpace(couponCode) != "" {
		vc, err := a.validateCouponForOrder(ctx, couponCode, scope, order.SubtotalCents, userID)
		if err != nil {
			return err
		}
		order.CouponID = &vc.Coupon.ID
		order.CouponCode = vc.Coupon.Code
		order.DiscountCents = vc.DiscountCents
	}
	if applyCreditCents > 0 && userID != nil {
		credit, err := a.applyCreditToOrder(ctx, *userID, applyCreditCents,
			order.SubtotalCents-order.DiscountCents)
		if err != nil {
			return err
		}
		order.CreditCents = credit
	}
	order.TotalCents = order.SubtotalCents - order.DiscountCents - order.CreditCents
	if order.TotalCents < 0 {
		order.TotalCents = 0
	}
	return nil
}

// consumeOrderDiscounts is the helper the payment-confirm paths call
// once they know an order is actually paid. It loads the order so the
// caller doesn't need to (the gateway-verify hooks only have an id)
// and then defers to recordOrderDiscounts.
func (a *API) consumeOrderDiscounts(ctx context.Context, orderID int64) {
	order, err := a.store.GetOrder(ctx, orderID)
	if err != nil || order == nil {
		return
	}
	var uid *int64
	if order.UserID != nil {
		v := *order.UserID
		uid = &v
	}
	a.recordOrderDiscounts(ctx, order, uid)
}

// recordOrderDiscounts writes the bookkeeping rows for an order that
// just transitioned into "paid" — coupon redemption + a negative
// credit-ledger entry.
//
// IMPORTANT: this is only called AFTER payment is confirmed, never at
// order creation time. Earlier code called this from createOrder,
// which let a customer drain their credit balance + burn single-use
// coupons by spamming pending orders.
//
// The function is idempotent: RecordCouponRedemption upserts on
// order_id, and the spend insert is gated on HasOrderSpend so a
// replayed webhook / repeated admin action doesn't double-debit.
// Errors are logged-only since the order is already considered paid.
func (a *API) recordOrderDiscounts(ctx context.Context, order *store.Order, userID *int64) {
	if order.CouponID != nil {
		_ = a.store.RecordCouponRedemption(ctx, *order.CouponID, userID, order.ID, order.DiscountCents)
	}
	if order.CreditCents > 0 && userID != nil {
		already, err := a.store.HasOrderSpend(ctx, order.ID)
		if err != nil || already {
			return
		}
		oid := order.ID
		_ = a.store.AddCreditTransaction(ctx, &store.CreditTransaction{
			UserID:         *userID,
			AmountCents:    -order.CreditCents,
			Reason:         "order_spend",
			RelatedOrderID: &oid,
		})
	}
}

// validateCoupon is the public endpoint the checkout page calls to preview
// the discount before submitting an order.
func (a *API) validateCoupon(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code          string `json:"code"`
		Scope         string `json:"scope"`
		SubtotalCents int64  `json:"subtotalCents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(in.Code) == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	scope := normaliseScope(in.Scope)
	var uid *int64
	if claims := a.optionalClaims(r); claims != nil {
		if id := parseClaimsUserID(claims); id > 0 {
			uid = &id
		}
	}
	res, err := a.validateCouponForOrder(r.Context(), in.Code, scope, in.SubtotalCents, uid)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":            res.Coupon.Code,
		"description":     res.Coupon.Description,
		"discountType":    res.Coupon.DiscountType,
		"percentOff":      res.Coupon.PercentOff,
		"amountOffCents":  res.Coupon.AmountOffCents,
		"scope":           res.Coupon.Scope,
		"discountCents":   res.DiscountCents,
	})
}

func normaliseScope(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "shop", "courses", "course", "memberships", "membership":
		if s == "course" {
			return "courses"
		}
		if s == "membership" {
			return "memberships"
		}
		return s
	}
	return "all"
}

// --- Admin CRUD ---

type couponInput struct {
	Code              string  `json:"code"`
	Description       string  `json:"description"`
	DiscountType      string  `json:"discountType"`
	PercentOff        int     `json:"percentOff"`
	AmountOffCents    int64   `json:"amountOffCents"`
	Scope             string  `json:"scope"`
	MinSubtotalCents  int64   `json:"minSubtotalCents"`
	MaxUses           *int    `json:"maxUses"`
	PerUserMaxUses    *int    `json:"perUserMaxUses"`
	StartsAt          *string `json:"startsAt"`
	ExpiresAt         *string `json:"expiresAt"`
	Active            bool    `json:"active"`
}

func (in couponInput) toCoupon() (*store.Coupon, error) {
	c := &store.Coupon{
		Code:             strings.ToUpper(strings.TrimSpace(in.Code)),
		Description:      strings.TrimSpace(in.Description),
		DiscountType:     strings.ToLower(strings.TrimSpace(in.DiscountType)),
		PercentOff:       in.PercentOff,
		AmountOffCents:   in.AmountOffCents,
		Scope:            normaliseScope(in.Scope),
		MinSubtotalCents: in.MinSubtotalCents,
		MaxUses:          in.MaxUses,
		PerUserMaxUses:   in.PerUserMaxUses,
		Active:           in.Active,
	}
	if c.Code == "" {
		return nil, fmt.Errorf("code is required")
	}
	switch c.DiscountType {
	case "percent":
		if c.PercentOff <= 0 || c.PercentOff > 100 {
			return nil, fmt.Errorf("percentOff must be between 1 and 100")
		}
		c.AmountOffCents = 0
	case "amount":
		if c.AmountOffCents <= 0 {
			return nil, fmt.Errorf("amountOffCents must be greater than 0")
		}
		c.PercentOff = 0
	default:
		return nil, fmt.Errorf("discountType must be 'percent' or 'amount'")
	}
	if in.StartsAt != nil && *in.StartsAt != "" {
		t, err := time.Parse(time.RFC3339, *in.StartsAt)
		if err != nil {
			return nil, fmt.Errorf("startsAt must be an RFC3339 timestamp")
		}
		c.StartsAt = &t
	}
	if in.ExpiresAt != nil && *in.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *in.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("expiresAt must be an RFC3339 timestamp")
		}
		c.ExpiresAt = &t
	}
	return c, nil
}

func (a *API) listAdminCoupons(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListCoupons(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) createAdminCoupon(w http.ResponseWriter, r *http.Request) {
	var in couponInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := in.toCoupon()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	uid := currentUserID(r)
	c.CreatedBy = &uid
	if err := a.store.CreateCoupon(r.Context(), c); err != nil {
		// Postgres unique-violation surfaces as a generic error; map it to
		// something useful for the admin UI.
		if strings.Contains(err.Error(), "coupons_code_key") {
			writeError(w, http.StatusConflict, "a coupon with that code already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) updateAdminCoupon(w http.ResponseWriter, r *http.Request) {
	existing, err := a.store.GetCouponByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "coupon not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in couponInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	parsed, err := in.toCoupon()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	parsed.ID = existing.ID
	parsed.UsedCount = existing.UsedCount
	parsed.CreatedAt = existing.CreatedAt
	parsed.CreatedBy = existing.CreatedBy
	if err := a.store.UpdateCoupon(r.Context(), parsed); err != nil {
		if strings.Contains(err.Error(), "coupons_code_key") {
			writeError(w, http.StatusConflict, "a coupon with that code already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, parsed)
}

func (a *API) deleteAdminCoupon(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteCoupon(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "coupon not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
