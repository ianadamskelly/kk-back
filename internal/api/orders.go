package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

// createOrder places a customer order from the public checkout. Line prices are
// taken from the database, never trusted from the client. Optional couponCode
// + applyCreditCents fields let the customer pre-apply discounts.
func (a *API) createOrder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CustomerName     string `json:"customerName"`
		CustomerEmail    string `json:"customerEmail"`
		CustomerPhone    string `json:"customerPhone"`
		Note             string `json:"note"`
		CouponCode       string `json:"couponCode"`
		ApplyCreditCents int64  `json:"applyCreditCents"`
		Items            []struct {
			ProductID int64 `json:"productId"`
			Quantity  int   `json:"quantity"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.CustomerName = strings.TrimSpace(in.CustomerName)
	in.CustomerEmail = strings.TrimSpace(in.CustomerEmail)
	if in.CustomerName == "" || !strings.Contains(in.CustomerEmail, "@") {
		writeError(w, http.StatusBadRequest, "a name and valid email are required")
		return
	}
	if len(in.Items) == 0 {
		writeError(w, http.StatusBadRequest, "your cart is empty")
		return
	}

	var items []store.OrderItem
	var subtotal int64
	for _, line := range in.Items {
		if line.Quantity < 1 {
			continue
		}
		product, err := a.store.GetProductByID(r.Context(), line.ProductID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && product.Status != "published") {
			writeError(w, http.StatusBadRequest, "a product in your cart is no longer available")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		pid := product.ID
		items = append(items, store.OrderItem{
			ProductID:      &pid,
			ProductName:    product.Name,
			UnitPriceCents: product.PriceCents,
			Quantity:       line.Quantity,
		})
		subtotal += product.PriceCents * int64(line.Quantity)
	}
	if len(items) == 0 {
		writeError(w, http.StatusBadRequest, "your cart is empty")
		return
	}

	var uid *int64
	if claims := a.optionalClaims(r); claims != nil {
		if id, err := strconv.ParseInt(claims.Subject, 10, 64); err == nil && id > 0 {
			uid = &id
		}
	}

	order := &store.Order{
		UserID:        uid,
		Kind:          "shop",
		CustomerName:  in.CustomerName,
		CustomerEmail: in.CustomerEmail,
		CustomerPhone: strings.TrimSpace(in.CustomerPhone),
		Note:          strings.TrimSpace(in.Note),
		SubtotalCents: subtotal,
		TotalCents:    subtotal,
	}
	if err := a.applyDiscountsAndCredit(r.Context(), order, uid, in.CouponCode, in.ApplyCreditCents, "shop"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := a.store.CreateOrder(r.Context(), order, items); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.recordOrderDiscounts(r.Context(), order, uid)

	writeJSON(w, http.StatusCreated, order)
}

// applyCreditToOrder returns the credit amount to apply, capped at both
// the user's balance and the post-discount subtotal.
func (a *API) applyCreditToOrder(ctx context.Context, userID, requested, maxApplicable int64) (int64, error) {
	if requested <= 0 {
		return 0, nil
	}
	balance, err := a.store.GetCreditBalance(ctx, userID)
	if err != nil {
		return 0, err
	}
	if balance <= 0 {
		return 0, nil
	}
	cap := requested
	if cap > balance {
		cap = balance
	}
	if cap > maxApplicable {
		cap = maxApplicable
	}
	if cap < 0 {
		cap = 0
	}
	return cap, nil
}

// listAccountOrders returns orders placed by the current signed-in user.
func (a *API) listAccountOrders(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	orders, err := a.store.ListUserOrders(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, orders)
}

func (a *API) listOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := a.store.ListOrders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, orders)
}

func (a *API) getOrder(w http.ResponseWriter, r *http.Request) {
	order, err := a.store.GetOrder(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (a *API) updateOrder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch in.Status {
	case "pending", "confirmed", "fulfilled", "cancelled":
	default:
		writeError(w, http.StatusBadRequest, "invalid order status")
		return
	}
	id := parseID(chi.URLParam(r, "id"))
	err := a.store.UpdateOrderStatus(r.Context(), id, in.Status)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// An admin marking an order confirmed should fire the same entitlement
	// + referral-reward side-effects as a gateway verify. This makes
	// off-gateway payments (bank transfer, manual reconciliation) work end-to-end.
	if in.Status == "confirmed" {
		a.applyEntitlements(r, id)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": in.Status})
}

func (a *API) deleteOrder(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteOrder(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
