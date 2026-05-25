package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

// initiatePayment starts a hosted-payment session for an order. The gateway
// can be picked via ?gateway=flutterwave|sifalo (default: flutterwave) and
// the currency via ?currency=USD|KES (default: USD). Returns either:
//
//	{ mode: "inline", publicKey, txRef, amount, currency, customer, ... } — Flutterwave
//	{ mode: "redirect", paymentUrl, txRef }                                — Sifalo
func (a *API) initiatePayment(w http.ResponseWriter, r *http.Request) {
	gateway := strings.ToLower(r.URL.Query().Get("gateway"))
	if gateway == "" {
		gateway = "flutterwave"
	}
	currency := strings.ToUpper(r.URL.Query().Get("currency"))
	if currency == "" {
		currency = "USD"
	}
	switch gateway {
	case "flutterwave":
		if a.cfg.FlutterwavePublicKey == "" {
			writeError(w, http.StatusServiceUnavailable, "flutterwave is not configured")
			return
		}
	case "sifalo":
		if a.cfg.SifalopayAPIUser == "" || a.cfg.SifalopayAPIKey == "" {
			writeError(w, http.StatusServiceUnavailable, "sifalo pay is not configured")
			return
		}
		currency = "USD" // Sifalo Checkout only supports USD.
	default:
		writeError(w, http.StatusBadRequest, "unknown gateway")
		return
	}
	if currency != "USD" && currency != "KES" {
		writeError(w, http.StatusBadRequest, "currency must be USD or KES")
		return
	}

	orderID := parseID(chi.URLParam(r, "id"))
	order, err := a.store.GetOrder(r.Context(), orderID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if last, err := a.store.GetLatestPaymentForOrder(r.Context(), order.ID); err == nil &&
		last != nil && last.Status == "successful" {
		writeError(w, http.StatusConflict, "order has already been paid")
		return
	}

	txRef, err := newTxRef(order.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate reference")
		return
	}

	switch gateway {
	case "flutterwave":
		a.initiateFlutterwaveInline(w, r, order, txRef, currency)
	case "sifalo":
		a.initiateSifalo(w, r, order, txRef)
	}
}

// amountForCurrency returns the order total expressed in the requested
// currency, in both major-unit float and minor-unit cents.
func (a *API) amountForCurrency(order *store.Order, currency string) (float64, int64) {
	if currency == "KES" {
		return float64(order.TotalCents) / 100, order.TotalCents
	}
	// USD — convert KES → USD.
	usd := float64(order.TotalCents) / 100 / a.cfg.KESPerUSD
	if usd < 0.01 {
		usd = 0.01
	}
	cents := int64(math.Round(usd * 100))
	return float64(cents) / 100, cents
}

// initiateFlutterwaveInline prepares an Inline Flutterwave checkout: it just
// creates the pending payment row and returns the public key + payment params
// to the browser, which then invokes FlutterwaveCheckout() directly. This
// avoids the server-to-Flutterwave initiate call entirely — handy for hosts
// that get challenged by Cloudflare on outbound requests, and faster overall.
func (a *API) initiateFlutterwaveInline(w http.ResponseWriter, r *http.Request, order *store.Order, txRef, currency string) {
	amount, amountCents := a.amountForCurrency(order, currency)

	payment := &store.Payment{
		OrderID:     order.ID,
		Gateway:     "flutterwave",
		TxRef:       txRef,
		AmountCents: amountCents,
		Currency:    currency,
		Status:      "pending",
	}
	if err := a.store.CreatePayment(r.Context(), payment); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"mode":      "inline",
		"gateway":   "flutterwave",
		"publicKey": a.cfg.FlutterwavePublicKey,
		"txRef":     txRef,
		"amount":    fmt.Sprintf("%.2f", amount),
		"currency":  currency,
		"customer": map[string]string{
			"name":  order.CustomerName,
			"email": order.CustomerEmail,
			"phone": order.CustomerPhone,
		},
		"title":       "Kuza Kizazi",
		"description": fmt.Sprintf("Order #%d", order.ID),
		"redirectUrl": a.cfg.PublicBaseURL + "/payment/complete?gateway=flutterwave",
		"meta":        map[string]any{"order_id": order.ID},
	})
}

func (a *API) initiateSifalo(w http.ResponseWriter, r *http.Request, order *store.Order, txRef string) {
	// Sifalo Checkout only supports USD — convert the KES order total using
	// the configured rate and store the USD-cents on the payment so verify
	// can compare amounts correctly.
	usd := float64(order.TotalCents) / 100 / a.cfg.KESPerUSD
	if usd < 0.01 {
		usd = 0.01
	}
	usdCents := int64(math.Round(usd * 100))

	payment := &store.Payment{
		OrderID:     order.ID,
		Gateway:     "sifalo",
		TxRef:       txRef,
		AmountCents: usdCents,
		Currency:    "USD",
		Status:      "pending",
	}
	if err := a.store.CreatePayment(r.Context(), payment); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	returnURL := a.cfg.PublicBaseURL +
		"/payment/complete?gateway=sifalo&order_id=" + url.QueryEscape(txRef)

	resp, err := a.sifaloInitiate(r.Context(), usd, returnURL)
	if err != nil {
		payment.Status = "failed"
		if resp != nil {
			payment.RawResponse = resp.Raw
		}
		_ = a.store.UpdatePayment(r.Context(), payment)
		writeError(w, http.StatusBadGateway, "could not start payment: "+err.Error())
		return
	}

	payment.RawResponse = resp.Raw
	_ = a.store.UpdatePayment(r.Context(), payment)

	checkoutURL := a.cfg.SifalopayCheckoutURL +
		"?key=" + url.QueryEscape(resp.Key) +
		"&token=" + url.QueryEscape(resp.Token)

	writeJSON(w, http.StatusOK, map[string]string{
		"mode":       "redirect",
		"gateway":    "sifalo",
		"paymentUrl": checkoutURL,
		"txRef":      txRef,
	})
}

// verifyPayment is called from the return URL after the customer completes
// (or cancels) checkout. It dispatches by the stored payment's gateway and
// promotes the order to "confirmed" on success.
func (a *API) verifyPayment(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ref := q.Get("tx_ref")
	if ref == "" {
		ref = q.Get("order_id")
	}
	if ref == "" {
		writeError(w, http.StatusBadRequest, "tx_ref or order_id is required")
		return
	}
	payment, err := a.store.GetPaymentByTxRef(r.Context(), ref)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "payment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if payment.Status == "successful" {
		writeJSON(w, http.StatusOK, payment)
		return
	}

	switch payment.Gateway {
	case "sifalo":
		if err := a.applySifaloVerify(r, payment, q.Get("sid")); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	default: // flutterwave
		if err := a.applyFlutterwaveVerify(r, payment); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, payment)
}

// flutterwaveWebhook is hit by Flutterwave when a transaction completes.
// We use it only as a trigger; the source of truth is the verify call.
func (a *API) flutterwaveWebhook(w http.ResponseWriter, r *http.Request) {
	hash := r.Header.Get("verif-hash")
	if a.cfg.FlutterwaveSecretHash == "" || hash != a.cfg.FlutterwaveSecretHash {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}
	body, _ := io.ReadAll(r.Body)
	var event struct {
		Event string `json:"event"`
		Data  struct {
			TxRef  string `json:"tx_ref"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil || event.Data.TxRef == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	payment, err := a.store.GetPaymentByTxRef(r.Context(), event.Data.TxRef)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = a.applyFlutterwaveVerify(r, payment)
	w.WriteHeader(http.StatusOK)
}

// applyFlutterwaveVerify calls Flutterwave's verify endpoint and updates the
// payment + parent order accordingly.
func (a *API) applyFlutterwaveVerify(r *http.Request, payment *store.Payment) error {
	resp, err := a.flutterwaveVerify(r.Context(), payment.TxRef)
	if err != nil {
		return err
	}
	expectedCents := payment.AmountCents
	gotCents := int64(math.Round(resp.Data.Amount * 100))

	switch {
	case resp.Status == "success" && resp.Data.Status == "successful" &&
		gotCents >= expectedCents && resp.Data.Currency == payment.Currency:
		now := time.Now().UTC()
		payment.Status = "successful"
		payment.ProviderTxID = strconv.FormatInt(resp.Data.ID, 10)
		payment.VerifiedAt = &now
		payment.RawResponse = resp.Raw
		if err := a.store.UpdatePayment(r.Context(), payment); err != nil {
			return err
		}
		_ = a.store.UpdateOrderStatus(r.Context(), payment.OrderID, "confirmed")
		a.applyEntitlements(r, payment.OrderID)
	case resp.Data.Status == "failed" || resp.Data.Status == "cancelled":
		payment.Status = resp.Data.Status
		payment.RawResponse = resp.Raw
		_ = a.store.UpdatePayment(r.Context(), payment)
	default:
		payment.RawResponse = resp.Raw
		_ = a.store.UpdatePayment(r.Context(), payment)
	}
	return nil
}

// applySifaloVerify checks a Sifalo payment via the gateway's verify endpoint
// and updates the payment + parent order. Prefers the sid passed back on the
// return URL; falls back to our internal tx_ref used as Sifalo's order_id.
func (a *API) applySifaloVerify(r *http.Request, payment *store.Payment, sid string) error {
	resp, err := a.sifaloVerify(r.Context(), sid, payment.TxRef)
	if err != nil {
		return err
	}

	// Amount tolerance: payment.AmountCents is in USD cents for Sifalo.
	gotUSD, _ := strconv.ParseFloat(resp.Amount, 64)
	gotCents := int64(math.Round(gotUSD * 100))
	expectedCents := payment.AmountCents

	switch strings.ToLower(resp.Status) {
	case "success":
		if gotCents+1 < expectedCents { // allow 1-cent rounding slack
			payment.Status = "failed"
			payment.RawResponse = resp.Raw
			_ = a.store.UpdatePayment(r.Context(), payment)
			return fmt.Errorf("sifalo amount mismatch: got %s expected %.2f", resp.Amount, float64(expectedCents)/100)
		}
		now := time.Now().UTC()
		payment.Status = "successful"
		payment.ProviderTxID = resp.SID
		payment.VerifiedAt = &now
		payment.RawResponse = resp.Raw
		if err := a.store.UpdatePayment(r.Context(), payment); err != nil {
			return err
		}
		_ = a.store.UpdateOrderStatus(r.Context(), payment.OrderID, "confirmed")
		a.applyEntitlements(r, payment.OrderID)
	case "failure":
		payment.Status = "failed"
		payment.RawResponse = resp.Raw
		_ = a.store.UpdatePayment(r.Context(), payment)
	default: // "pending" or unknown
		payment.RawResponse = resp.Raw
		_ = a.store.UpdatePayment(r.Context(), payment)
	}
	return nil
}

// applyEntitlements grants whatever access a freshly-confirmed order earns:
// for membership orders, extend the user's membership by 30 days. Course
// orders need no extra work — entitlement is implicit via the order_item.
// We also kick off the referral-reward check here so the referrer gets
// store credit on the referee's first paid order.
// Best-effort: errors here are swallowed so a transient failure doesn't undo
// a payment that has already been marked successful.
func (a *API) applyEntitlements(r *http.Request, orderID int64) {
	order, err := a.store.GetOrder(r.Context(), orderID)
	if err != nil || order == nil {
		return
	}
	if order.Kind == "membership" && order.UserID != nil {
		_, _ = a.store.ExtendMembership(r.Context(), *order.UserID, 30*24*time.Hour)
	}
	a.maybeGrantReferralReward(r, order)
	a.tagSubscriberFromOrder(r, order)
}

// tagSubscriberFromOrder ensures the order's customer is on the mailing
// list and carries the right source tag (shop / courses / membership /
// service). Guest checkouts get an entry too — we have their email.
func (a *API) tagSubscriberFromOrder(r *http.Request, order *store.Order) {
	if order.CustomerEmail == "" {
		return
	}
	tag := order.Kind
	if tag == "" {
		tag = "shop"
	}
	var uid *int64
	if order.UserID != nil {
		v := *order.UserID
		uid = &v
	}
	_, _ = a.store.UpsertSubscriberWithTags(r.Context(), store.SubscriberUpsert{
		Email:  order.CustomerEmail,
		Name:   order.CustomerName,
		Source: tag,
		UserID: uid,
		Tags:   []string{tag, "customer"},
	})
}

// maybeGrantReferralReward credits the referrer when their referee
// completes their first paid order. The reward fires once per referee
// (guarded by users.referral_rewarded_at) and only when the order's
// post-discount total is strictly positive (so 100%-coupon farming
// doesn't earn rewards).
func (a *API) maybeGrantReferralReward(r *http.Request, order *store.Order) {
	if order.UserID == nil || order.TotalCents <= 0 {
		return
	}
	referee, err := a.store.GetUserByID(r.Context(), *order.UserID)
	if err != nil || referee == nil || referee.ReferredByUserID == nil {
		return
	}
	if referee.ReferralRewardedAt != nil {
		return // already rewarded for this referee
	}
	// Double-check it's actually their first paid order (defensive).
	if had, err := a.store.HasFirstPaidOrder(r.Context(), referee.ID, order.ID); err == nil && had {
		return
	}
	reward := a.referralRewardCents(r.Context())
	if reward <= 0 {
		return
	}
	related := referee.ID
	if err := a.store.AddCreditTransaction(r.Context(), &store.CreditTransaction{
		UserID:        *referee.ReferredByUserID,
		AmountCents:   reward,
		Reason:        "referral_reward",
		RelatedUserID: &related,
		Note:          "Referral reward for " + referee.Email,
	}); err != nil {
		return
	}
	_ = a.store.MarkRefereeRewarded(r.Context(), referee.ID)
}

// newTxRef returns a unique reference like "kk-{order}-{unix}-{rand6}".
func newTxRef(orderID int64) (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("kk-%d-%d-%s", orderID, time.Now().Unix(), hex.EncodeToString(buf)), nil
}
