package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Sifalo Pay's hosted Checkout flow:
//   1. POST /gateway/ with Basic Auth + {amount, gateway:"checkout", currency:"USD", return_url}
//      → response { key, token }
//   2. Redirect customer to <checkoutURL>?key=…&token=…
//   3. After payment, Sifalo redirects back to return_url with ?sid=<transactionID>
//   4. POST /gateway/verify.php with Basic Auth + {"sid": …} (or {"order_id": …})
//      → response { sid, account, payment_type, amount, status, code }
//
// Checkout currently only supports USD, so we convert the KES order total to
// USD using cfg.KESPerUSD before sending.

type sifaloAuthResp struct {
	Key   string          `json:"key"`
	Token string          `json:"token"`
	Raw   json.RawMessage `json:"-"`
}

type sifaloVerifyResp struct {
	SID         string          `json:"sid"`
	Account     string          `json:"account"`
	PaymentType string          `json:"payment_type"`
	Amount      string          `json:"amount"`
	Status      string          `json:"status"` // "success" | "failure" | "pending"
	Code        int             `json:"code"`
	Raw         json.RawMessage `json:"-"`
}

func (a *API) sifaloBasicAuth() string {
	creds := a.cfg.SifalopayAPIUser + ":" + a.cfg.SifalopayAPIKey
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}

// sifaloInitiate authenticates a Checkout session and returns a key+token pair
// that the frontend can use to build the hosted-checkout URL.
func (a *API) sifaloInitiate(ctx context.Context, amountUSD float64, returnURL, orderID string) (*sifaloAuthResp, error) {
	body := map[string]any{
		"amount":     fmt.Sprintf("%.2f", amountUSD),
		"gateway":    "checkout",
		"currency":   "USD",
		"return_url": returnURL,
	}
	if orderID != "" {
		body["order_id"] = orderID
	}
	raw, _ := json.Marshal(body)

	respBody, err := curlJSON(ctx, "POST", a.cfg.SifalopayBaseURL, a.sifaloBasicAuth(), raw)
	if err != nil {
		return nil, err
	}
	var out sifaloAuthResp
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("sifalo: bad response: %s", string(respBody))
	}
	out.Raw = respBody
	if out.Key == "" || out.Token == "" {
		return &out, fmt.Errorf("sifalo: missing key/token in response: %s", string(respBody))
	}
	return &out, nil
}

// sifaloVerify checks a payment's status. Prefer the Sifalo sid; fall back to
// the merchant order_id (our internal tx_ref) when the sid is unavailable.
func (a *API) sifaloVerify(ctx context.Context, sid, orderID string) (*sifaloVerifyResp, error) {
	body := map[string]any{}
	if sid != "" {
		body["sid"] = sid
	} else if orderID != "" {
		body["order_id"] = orderID
	} else {
		return nil, fmt.Errorf("sifalo verify: sid or order_id is required")
	}
	raw, _ := json.Marshal(body)

	respBody, err := curlJSON(ctx, "POST", a.cfg.SifalopayVerifyURL, a.sifaloBasicAuth(), raw)
	if err != nil {
		return nil, err
	}
	var out sifaloVerifyResp
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("sifalo verify: bad response: %s", string(respBody))
	}
	out.Raw = respBody
	return &out, nil
}
