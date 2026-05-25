package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
)

// We shell out to the system `curl` for Flutterwave calls. Their CDN
// (Cloudflare) challenges Go's net/http TLS+HTTP fingerprint reliably enough
// that even uTLS with a Chrome handshake gets blocked. `curl` is universally
// available in production environments and Cloudflare is consistent in
// allowing its fingerprint.

// flutterwaveInitiateReq is the data needed to start a hosted-payment session.
type flutterwaveInitiateReq struct {
	TxRef         string
	Amount        string // major units, e.g. "1500.00"
	Currency      string
	RedirectURL   string
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
	Title         string
	Description   string
	Meta          map[string]any
}

// flutterwaveInitiateResp is the subset of /payments we care about.
type flutterwaveInitiateResp struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Link string `json:"link"`
	} `json:"data"`
	Raw json.RawMessage `json:"-"`
}

// flutterwaveVerifyResp models the /transactions/verify_by_reference response.
type flutterwaveVerifyResp struct {
	Status string `json:"status"`
	Data   struct {
		ID        int64   `json:"id"`
		TxRef     string  `json:"tx_ref"`
		Status    string  `json:"status"` // "successful" on success
		Amount    float64 `json:"amount"`
		Currency  string  `json:"currency"`
		ChargedAt string  `json:"created_at"`
	} `json:"data"`
	Raw json.RawMessage `json:"-"`
}

// curlJSON executes `curl` with the given method, url, optional JSON body,
// and bearer token, returning the response body and exit error.
func curlJSON(ctx context.Context, method, url, authHeader string, body []byte) ([]byte, error) {
	args := []string{
		"-sS",
		"--max-time", "20",
		"-A", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"-H", "Authorization: " + authHeader,
	}
	if body != nil {
		args = append(args,
			"-H", "Content-Type: application/json",
			"--data-binary", string(body),
		)
	} else if method != "GET" {
		args = append(args, "-X", method)
	}
	args = append(args, url)

	cmd := exec.CommandContext(ctx, "curl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("curl: %v (%s)", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// flutterwaveInitiate creates a hosted-payment session and returns the link.
func (a *API) flutterwaveInitiate(ctx context.Context, in flutterwaveInitiateReq) (*flutterwaveInitiateResp, error) {
	body := map[string]any{
		"tx_ref":       in.TxRef,
		"amount":       in.Amount,
		"currency":     in.Currency,
		"redirect_url": in.RedirectURL,
		"customer": map[string]any{
			"email":       in.CustomerEmail,
			"name":        in.CustomerName,
			"phonenumber": in.CustomerPhone,
		},
		"customizations": map[string]any{
			"title":       in.Title,
			"description": in.Description,
		},
		"meta": in.Meta,
	}
	raw, _ := json.Marshal(body)

	respBody, err := curlJSON(ctx, "POST",
		a.cfg.FlutterwaveBaseURL+"/payments",
		"Bearer "+a.cfg.FlutterwaveSecretKey, raw)
	if err != nil {
		return nil, err
	}

	var out flutterwaveInitiateResp
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("flutterwave: bad response: %s", string(respBody))
	}
	out.Raw = respBody
	if out.Status != "success" {
		return &out, fmt.Errorf("flutterwave: %s", out.Message)
	}
	return &out, nil
}

// flutterwaveVerify checks the live status of a transaction by tx_ref.
func (a *API) flutterwaveVerify(ctx context.Context, txRef string) (*flutterwaveVerifyResp, error) {
	q := url.Values{}
	q.Set("tx_ref", txRef)
	respBody, err := curlJSON(ctx, "GET",
		a.cfg.FlutterwaveBaseURL+"/transactions/verify_by_reference?"+q.Encode(),
		"Bearer "+a.cfg.FlutterwaveSecretKey, nil)
	if err != nil {
		return nil, err
	}

	var out flutterwaveVerifyResp
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("flutterwave verify: bad response: %s", string(respBody))
	}
	out.Raw = respBody
	return &out, nil
}
