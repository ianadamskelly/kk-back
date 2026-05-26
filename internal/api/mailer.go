package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"html"
	"log"
	"net/smtp"
	"strconv"
	"strings"
	"sync/atomic"
)

// Mailer is the small interface the API uses to send transactional email.
// A nil mailer means "SMTP not configured" — callers are expected to
// degrade gracefully (e.g. invites stay copy-the-link only, welcome
// emails are silently skipped, newsletters can be drafted but not sent).
type Mailer interface {
	SendInvite(ctx context.Context, toEmail, toName, roleName, inviteURL string) error
	SendWelcome(ctx context.Context, toEmail, toName string) error
	// SendNewsletter sends one personalised copy. The unsubscribeURL is
	// rendered verbatim into the footer so each recipient gets their own
	// token. bodyHTML is the admin-written rich-text content.
	SendNewsletter(ctx context.Context, toEmail, toName, subject, bodyHTML, unsubscribeURL string) error
	// SendOrderConfirmation greets a buyer immediately after they place
	// an order. The amount is formatted by the caller (KES/USD aware).
	SendOrderConfirmation(ctx context.Context, toEmail, toName string, summary OrderEmailSummary) error
	// SendOrderFulfilled tells the buyer the order is ready. For digital
	// products, downloads[] carries pre-signed direct links.
	SendOrderFulfilled(ctx context.Context, toEmail, toName string, summary OrderEmailSummary, downloads []EmailDownloadLink) error
}

// OrderEmailSummary is the minimum every order-email needs to render
// its body. Built once by the API layer so the mailer doesn't have to
// know about the store package.
type OrderEmailSummary struct {
	OrderID       int64
	CustomerName  string
	Lines         []OrderEmailLine // product, quantity, line total (already formatted)
	TotalFormatted string          // e.g. "KSh 4,500" — caller decides currency
	AccountURL    string           // link to /account/orders on the frontend
}

// OrderEmailLine is one line item, pre-formatted for display.
type OrderEmailLine struct {
	Name      string
	Quantity  int
	Subtotal  string // e.g. "KSh 2,500"
}

// EmailDownloadLink is one downloadable file with its signed, time-
// limited URL. The mailer doesn't sign these — it just renders them.
type EmailDownloadLink struct {
	Label     string
	URL       string // absolute, includes API_PUBLIC_URL
	SizeHuman string // e.g. "2.4 MB" or "" if unknown
}

// SMTPMailer sends mail via a configured SMTP server.
type SMTPMailer struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // "Kuza Kizazi <hi@kuzakizazi.com>" or just an address
	UseTLS   bool   // true → STARTTLS / implicit TLS depending on port
}

// NewMailer constructs an SMTPMailer if all required env vars are set,
// otherwise returns nil — letting the admin UI offer copy-the-link as a
// fallback rather than failing the invite outright.
func NewMailer(host, port, user, pass, from string, tlsEnabled bool) Mailer {
	if host == "" || port == "" || from == "" {
		log.Println("mailer: SMTP not fully configured — invites will be available as copy-the-link only")
		return nil
	}
	p, err := strconv.Atoi(port)
	if err != nil || p <= 0 {
		log.Printf("mailer: invalid SMTP_PORT %q — disabling email", port)
		return nil
	}
	return &SMTPMailer{
		Host:     host,
		Port:     p,
		Username: user,
		Password: pass,
		From:     from,
		UseTLS:   tlsEnabled,
	}
}

// SendWelcome greets a freshly-registered customer.
func (m *SMTPMailer) SendWelcome(_ context.Context, toEmail, toName string) error {
	subject := "Welcome to Kuza Kizazi"
	greeting := strings.TrimSpace(toName)
	if greeting == "" {
		greeting = "Hello"
	} else {
		greeting = "Hi " + greeting
	}

	htmlBody := fmt.Sprintf(`<!doctype html><html><body style="font-family:system-ui,sans-serif;color:#222;line-height:1.6">
<p>%s,</p>
<p>Welcome to <strong>Kuza Kizazi</strong> — we're glad to have you with us.</p>
<p>Your account is ready. From here you can browse the shop, enrol in courses, and follow the work of our team. We'll send you the occasional newsletter with what's new; you can unsubscribe any time from the footer of those emails.</p>
<p>If you have a question, just reply to this email — a real person reads it.</p>
<p>— The Kuza Kizazi team</p>
</body></html>`, html.EscapeString(greeting))

	textBody := fmt.Sprintf("%s,\n\nWelcome to Kuza Kizazi — we're glad to have you with us.\n\nYour account is ready. Browse the shop, enrol in courses, and follow our team's work. We'll send you the occasional newsletter; unsubscribe any time from the footer of those emails.\n\nIf you have a question, just reply to this email.\n\n— The Kuza Kizazi team",
		greeting)

	return m.send(toEmail, subject, textBody, htmlBody)
}

// SendNewsletter sends a single personalised copy. The unsubscribe URL is
// rendered into both the text and HTML footers verbatim.
func (m *SMTPMailer) SendNewsletter(_ context.Context, toEmail, toName, subject, bodyHTML, unsubscribeURL string) error {
	htmlBody := fmt.Sprintf(`<!doctype html><html><body style="font-family:system-ui,sans-serif;color:#222;line-height:1.6;max-width:640px;margin:0 auto;padding:0 16px">
%s
<hr style="margin-top:32px;border:0;border-top:1px solid #eee">
<p style="color:#888;font-size:12px;margin-top:16px">
You're receiving this because you signed up at Kuza Kizazi.<br>
<a href="%s" style="color:#888">Unsubscribe</a>
</p>
</body></html>`, bodyHTML, html.EscapeString(unsubscribeURL))

	textBody := fmt.Sprintf("%s\n\n— Kuza Kizazi\n\nUnsubscribe: %s",
		stripHTML(bodyHTML), unsubscribeURL)

	_ = toName // available if templates ever want to personalise the greeting
	return m.send(toEmail, subject, textBody, htmlBody)
}

// stripHTML produces a rough plain-text fallback from an HTML body. We
// only need it for the text/plain MIME part — readers that show HTML
// won't see it, so a coarse de-tag is fine.
func stripHTML(in string) string {
	out := strings.Builder{}
	inTag := false
	for _, r := range in {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			out.WriteRune(r)
		}
	}
	// Collapse runs of whitespace so the text version is readable.
	s := strings.Join(strings.Fields(out.String()), " ")
	return s
}

// SendOrderConfirmation greets a buyer with an order summary the
// moment their order is placed (still pending until payment lands).
func (m *SMTPMailer) SendOrderConfirmation(_ context.Context, toEmail, toName string, s OrderEmailSummary) error {
	subject := fmt.Sprintf("Order #%d received — Kuza Kizazi", s.OrderID)
	greeting := buildGreeting(toName)

	var rowsHTML, rowsText strings.Builder
	for _, l := range s.Lines {
		rowsHTML.WriteString(fmt.Sprintf(
			`<tr><td style="padding:6px 0">%s × %d</td><td style="padding:6px 0;text-align:right">%s</td></tr>`,
			html.EscapeString(l.Name), l.Quantity, html.EscapeString(l.Subtotal)))
		rowsText.WriteString(fmt.Sprintf("  - %s × %d — %s\n", l.Name, l.Quantity, l.Subtotal))
	}

	htmlBody := fmt.Sprintf(`<!doctype html><html><body style="font-family:system-ui,sans-serif;color:#222;line-height:1.6;max-width:640px;margin:0 auto;padding:0 16px">
<p>%s,</p>
<p>Thanks for your order — we&rsquo;ve received <strong>order #%d</strong> and it&rsquo;s now awaiting payment confirmation.</p>
<table style="width:100%%;border-collapse:collapse;margin:16px 0;font-size:14px">
%s
<tr><td style="padding-top:10px;border-top:1px solid #eee"><strong>Total</strong></td><td style="padding-top:10px;border-top:1px solid #eee;text-align:right"><strong>%s</strong></td></tr>
</table>
<p>You can follow the order&rsquo;s status from your account: <a href="%s">%s</a></p>
<p>— The Kuza Kizazi team</p>
</body></html>`,
		html.EscapeString(greeting), s.OrderID, rowsHTML.String(),
		html.EscapeString(s.TotalFormatted), html.EscapeString(s.AccountURL), html.EscapeString(s.AccountURL))

	textBody := fmt.Sprintf("%s,\n\nThanks for your order — we've received order #%d and it's now awaiting payment confirmation.\n\n%sTotal: %s\n\nFollow your order at: %s\n\n— The Kuza Kizazi team",
		greeting, s.OrderID, rowsText.String(), s.TotalFormatted, s.AccountURL)

	return m.send(toEmail, subject, textBody, htmlBody)
}

// SendOrderFulfilled tells the buyer their order is ready. For digital
// products, the downloads list embeds direct download buttons; for
// physical products it's an "on its way" note.
func (m *SMTPMailer) SendOrderFulfilled(_ context.Context, toEmail, toName string, s OrderEmailSummary, downloads []EmailDownloadLink) error {
	subject := fmt.Sprintf("Order #%d ready — Kuza Kizazi", s.OrderID)
	greeting := buildGreeting(toName)

	var rowsHTML, rowsText strings.Builder
	for _, l := range s.Lines {
		rowsHTML.WriteString(fmt.Sprintf(
			`<tr><td style="padding:6px 0">%s × %d</td><td style="padding:6px 0;text-align:right">%s</td></tr>`,
			html.EscapeString(l.Name), l.Quantity, html.EscapeString(l.Subtotal)))
		rowsText.WriteString(fmt.Sprintf("  - %s × %d — %s\n", l.Name, l.Quantity, l.Subtotal))
	}

	var dlHTML, dlText strings.Builder
	if len(downloads) > 0 {
		dlHTML.WriteString(`<h3 style="margin-top:24px">Your downloads</h3><ul style="padding-left:18px">`)
		dlText.WriteString("\nYour downloads:\n")
		for _, d := range downloads {
			label := d.Label
			if d.SizeHuman != "" {
				label = fmt.Sprintf("%s (%s)", d.Label, d.SizeHuman)
			}
			dlHTML.WriteString(fmt.Sprintf(`<li style="margin:6px 0"><a href="%s" style="color:#e7572f;font-weight:600">⬇ %s</a></li>`,
				html.EscapeString(d.URL), html.EscapeString(label)))
			dlText.WriteString(fmt.Sprintf("  - %s: %s\n", label, d.URL))
		}
		dlHTML.WriteString(`</ul><p style="font-size:13px;color:#888">These links are personal to you and stay valid for 7 days. After that, head to your account to fetch fresh links any time.</p>`)
		dlText.WriteString("\nThese links are personal and valid for 7 days; fetch fresh links from your account any time after that.\n")
	}

	bodyNote := "Your order has been fulfilled."
	if len(downloads) > 0 {
		bodyNote = "Your order has been fulfilled and your downloads are ready below."
	}

	htmlBody := fmt.Sprintf(`<!doctype html><html><body style="font-family:system-ui,sans-serif;color:#222;line-height:1.6;max-width:640px;margin:0 auto;padding:0 16px">
<p>%s,</p>
<p>%s</p>
<table style="width:100%%;border-collapse:collapse;margin:16px 0;font-size:14px">
%s
<tr><td style="padding-top:10px;border-top:1px solid #eee"><strong>Total</strong></td><td style="padding-top:10px;border-top:1px solid #eee;text-align:right"><strong>%s</strong></td></tr>
</table>
%s
<p>Manage your purchases at <a href="%s">%s</a></p>
<p>— The Kuza Kizazi team</p>
</body></html>`,
		html.EscapeString(greeting), html.EscapeString(bodyNote), rowsHTML.String(),
		html.EscapeString(s.TotalFormatted), dlHTML.String(),
		html.EscapeString(s.AccountURL), html.EscapeString(s.AccountURL))

	textBody := fmt.Sprintf("%s,\n\n%s\n\n%sTotal: %s\n%s\nManage your purchases at %s\n\n— The Kuza Kizazi team",
		greeting, bodyNote, rowsText.String(), s.TotalFormatted, dlText.String(), s.AccountURL)

	return m.send(toEmail, subject, textBody, htmlBody)
}

// buildGreeting returns "Hi <name>," or "Hello," when no name is known.
func buildGreeting(toName string) string {
	g := strings.TrimSpace(toName)
	if g == "" {
		return "Hello"
	}
	return "Hi " + g
}

// SendInvite renders and sends the invitation email.
func (m *SMTPMailer) SendInvite(_ context.Context, toEmail, toName, roleName, inviteURL string) error {
	subject := "You're invited to join Kuza Kizazi"
	greeting := strings.TrimSpace(toName)
	if greeting == "" {
		greeting = "Hello"
	} else {
		greeting = "Hi " + greeting
	}

	htmlBody := fmt.Sprintf(`<!doctype html><html><body style="font-family:system-ui,sans-serif;color:#222;line-height:1.5">
<p>%s,</p>
<p>You've been invited to join the <strong>Kuza Kizazi</strong> admin panel as a <strong>%s</strong>.</p>
<p>Set your password and finish setting up your account by clicking the button below.</p>
<p><a href="%s" style="display:inline-block;background:#e7572f;color:#fff;text-decoration:none;font-weight:600;padding:12px 22px;border-radius:999px">Accept your invitation</a></p>
<p>Or copy this link into your browser:<br><a href="%s">%s</a></p>
<p style="color:#888;font-size:13px;margin-top:32px">This invitation expires in 7 days. If you weren't expecting it you can safely ignore this email.</p>
</body></html>`, html.EscapeString(greeting), html.EscapeString(roleName),
		html.EscapeString(inviteURL), html.EscapeString(inviteURL), html.EscapeString(inviteURL))

	textBody := fmt.Sprintf("%s,\n\nYou've been invited to join the Kuza Kizazi admin panel as a %s.\n\nAccept the invitation here:\n%s\n\nThis link expires in 7 days.",
		greeting, roleName, inviteURL)

	return m.send(toEmail, subject, textBody, htmlBody)
}

func (m *SMTPMailer) send(to, subject, textBody, htmlBody string) error {
	boundary := "kk-boundary-" + randomBoundary()
	headers := []string{
		"From: " + m.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=" + boundary,
	}
	var body strings.Builder
	body.WriteString(strings.Join(headers, "\r\n") + "\r\n\r\n")
	body.WriteString("--" + boundary + "\r\n")
	body.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
	body.WriteString(textBody + "\r\n")
	body.WriteString("--" + boundary + "\r\n")
	body.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n\r\n")
	body.WriteString(htmlBody + "\r\n")
	body.WriteString("--" + boundary + "--\r\n")

	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	var auth smtp.Auth
	if m.Username != "" {
		auth = smtp.PlainAuth("", m.Username, m.Password, m.Host)
	}

	// Port 465 uses implicit TLS; everything else uses STARTTLS if enabled
	// or plain otherwise. smtp.SendMail handles STARTTLS upgrade for us.
	if m.UseTLS && m.Port == 465 {
		return sendImplicitTLS(addr, m.Host, auth, m.From, []string{to}, []byte(body.String()))
	}
	return smtp.SendMail(addr, auth, m.From, []string{to}, []byte(body.String()))
}

// sendImplicitTLS handles port-465-style SMTP-over-TLS where the connection
// is encrypted from the start (no STARTTLS upgrade).
func sendImplicitTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer c.Close()
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := c.Rcpt(recipient); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// boundaryCounter monotonically increments to keep MIME boundaries distinct
// across concurrent emails.
var boundaryCounter atomic.Int64

func randomBoundary() string {
	return strconv.FormatInt(boundaryCounter.Add(1), 36)
}
