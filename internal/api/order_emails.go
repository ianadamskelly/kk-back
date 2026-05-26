package api

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"kuzakizazi/internal/store"
)

// formatKSh formats integer cents as "KSh 1,234". KES is the
// default retail currency on the site; the public total is whole
// shillings (cents/100). If we ever multi-currency the order table,
// take this through Config instead.
func formatKSh(cents int64) string {
	amount := cents / 100
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}
	s := strconv.FormatInt(amount, 10)
	n := len(s)
	if n <= 3 {
		return "KSh " + sign + s
	}
	var b strings.Builder
	first := n % 3
	if first > 0 {
		b.WriteString(s[:first])
		b.WriteByte(',')
	}
	for i := first; i < n; i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < n {
			b.WriteByte(',')
		}
	}
	return "KSh " + sign + b.String()
}

// humanBytes returns "2.4 MB" / "812 KB" style sizes for emails.
func humanBytes(n int64) string {
	if n <= 0 {
		return ""
	}
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%d KB", n/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// buildOrderSummary turns a store.Order into the lightweight shape the
// mailer renders. Pure — no DB calls — so it's safe to call from
// goroutines that don't share a request context.
func (a *API) buildOrderSummary(o *store.Order) OrderEmailSummary {
	lines := make([]OrderEmailLine, 0, len(o.Items))
	for _, it := range o.Items {
		lines = append(lines, OrderEmailLine{
			Name:     it.ProductName,
			Quantity: it.Quantity,
			Subtotal: formatKSh(it.UnitPriceCents * int64(it.Quantity)),
		})
	}
	return OrderEmailSummary{
		OrderID:        o.ID,
		CustomerName:   o.CustomerName,
		Lines:          lines,
		TotalFormatted: formatKSh(o.TotalCents),
		AccountURL:     strings.TrimRight(a.cfg.PublicBaseURL, "/") + "/account/orders",
	}
}

// buildOrderDownloadLinks resolves every digital file the buyer can
// download on this order and signs a fresh token per file. Returns
// nil for orders without a user (guest, can't be linked) or without
// any digital items.
func (a *API) buildOrderDownloadLinks(ctx context.Context, o *store.Order) []EmailDownloadLink {
	if o.UserID == nil {
		return nil
	}
	files, err := a.store.ListOrderDigitalDownloads(ctx, *o.UserID, o.ID)
	if err != nil {
		log.Printf("order %d: could not list digital downloads: %v", o.ID, err)
		return nil
	}
	if len(files) == 0 {
		return nil
	}
	apiBase := strings.TrimRight(a.cfg.APIPublicURL, "/")
	out := make([]EmailDownloadLink, 0, len(files))
	for _, f := range files {
		token, err := a.signDownloadToken(*o.UserID, o.ID, f.DownloadID)
		if err != nil {
			log.Printf("order %d: could not sign download token for file %d: %v", o.ID, f.DownloadID, err)
			continue
		}
		out = append(out, EmailDownloadLink{
			Label:     f.Label,
			URL:       apiBase + "/api/downloads/" + token,
			SizeHuman: humanBytes(f.SizeBytes),
		})
	}
	return out
}

// sendOrderConfirmationEmailAsync fires the "we received your order"
// email in a goroutine. SMTP errors are logged and swallowed so a
// flaky relay never blocks order placement.
func (a *API) sendOrderConfirmationEmailAsync(o *store.Order) {
	if a.mailer == nil || o.CustomerEmail == "" {
		return
	}
	summary := a.buildOrderSummary(o)
	go func(email, name string, s OrderEmailSummary, orderID int64) {
		if err := a.mailer.SendOrderConfirmation(context.Background(), email, name, s); err != nil {
			log.Printf("order %d: confirmation email to %s failed: %v", orderID, email, err)
		}
	}(o.CustomerEmail, o.CustomerName, summary, o.ID)
}

// sendOrderFulfilledEmailAsync fires the "your order is ready" email
// in a goroutine. For digital products, the customer's signed
// download URLs are embedded directly so they can fetch the files
// without bouncing through the site.
func (a *API) sendOrderFulfilledEmailAsync(ctx context.Context, o *store.Order) {
	if a.mailer == nil || o.CustomerEmail == "" {
		return
	}
	summary := a.buildOrderSummary(o)
	downloads := a.buildOrderDownloadLinks(ctx, o)
	go func(email, name string, s OrderEmailSummary, dl []EmailDownloadLink, orderID int64) {
		if err := a.mailer.SendOrderFulfilled(context.Background(), email, name, s, dl); err != nil {
			log.Printf("order %d: fulfilment email to %s failed: %v", orderID, email, err)
		}
	}(o.CustomerEmail, o.CustomerName, summary, downloads, o.ID)
}

// orderHasOnlyDigitalItems is true when every line item in the order
// resolves to a published digital product. Used to decide whether to
// auto-fulfill on payment confirmation. Resolution failure (deleted
// product, etc.) is treated as "no" — admins handle those manually.
func (a *API) orderHasOnlyDigitalItems(ctx context.Context, o *store.Order) bool {
	if len(o.Items) == 0 {
		return false
	}
	for _, it := range o.Items {
		if it.ProductID == nil {
			return false
		}
		p, err := a.store.GetProductByID(ctx, *it.ProductID)
		if err != nil || p == nil {
			return false
		}
		if p.Kind != "digital" {
			return false
		}
	}
	return true
}

// autoFulfilDigitalOrder is called right after a payment is verified.
// For digital-only orders we move the status straight to "fulfilled"
// and fire the fulfilment email (with signed download links) so the
// customer doesn't have to wait for an admin to mark anything by hand.
// Mixed / all-physical orders are left at "confirmed"; admins issue
// the fulfilment email by marking the order fulfilled from the UI.
func (a *API) autoFulfilDigitalOrder(ctx context.Context, orderID int64) {
	o, err := a.store.GetOrder(ctx, orderID)
	if err != nil || o == nil {
		return
	}
	// Only shop orders carry product items with kinds. Course /
	// membership "orders" have no item lookup that makes sense here.
	if o.Kind != "shop" {
		return
	}
	if !a.orderHasOnlyDigitalItems(ctx, o) {
		return
	}
	if err := a.store.UpdateOrderStatus(ctx, orderID, "fulfilled"); err != nil {
		// Don't block the payment flow on this — log and move on.
		// The customer will still see their downloads on /account/downloads
		// (which works at status=confirmed too).
		// An admin can flip the status by hand to retry the email.
		return
	}
	o.Status = "fulfilled"
	a.sendOrderFulfilledEmailAsync(ctx, o)
}
