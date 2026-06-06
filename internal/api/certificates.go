package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-pdf/fpdf"
	qrcode "github.com/skip2/go-qrcode"

	"kuzakizazi/internal/store"
)

// crockfordAlphabet is Douglas Crockford's base32 — no ambiguous
// 0/O/1/I/L and no vowels (so codes can't spell words). Used for the
// printable certificate code.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// mintCertificateCode returns an opaque identifier like
// "KK-7Q4ZK-9MX2T-VW8RH" — ~50 bits of entropy from 10 random bytes,
// printable on the cert and usable as the URL slug. The extra length
// (vs the old 32-bit hex code) makes enumeration infeasible while
// staying legible. Lookups are exact-string, so older KK-<hex> codes
// remain valid.
func mintCertificateCode() (string, error) {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	// Encode each 5-bit group to a Crockford symbol (10 bytes = 80 bits
	// = 16 symbols), grouped 5-5-... wait we use 15 symbols below).
	var sb strings.Builder
	var acc uint32
	var bits int
	for _, b := range buf {
		acc = acc<<8 | uint32(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			sb.WriteByte(crockfordAlphabet[(acc>>uint(bits))&0x1f])
		}
	}
	if bits > 0 {
		sb.WriteByte(crockfordAlphabet[(acc<<uint(5-bits))&0x1f])
	}
	raw := sb.String() // 16 symbols
	// Group as KK-XXXXX-XXXXX-XXXXXX for readability.
	return "KK-" + raw[0:5] + "-" + raw[5:10] + "-" + raw[10:], nil
}

// ensureCertificate find-or-mints the (user, course) certificate. It's
// the single issuance path shared by every caller (grading auto-issue,
// manual admin issuance, and the student completion endpoint). Returns
// the certificate and whether it was newly created this call, so the
// caller can email exactly once.
func (a *API) ensureCertificate(ctx context.Context, userID, courseID int64) (*store.Certificate, bool, error) {
	if existing, _ := a.store.FindCertificate(ctx, userID, courseID); existing != nil {
		return existing, false, nil
	}
	code, err := mintCertificateCode()
	if err != nil {
		return nil, false, err
	}
	cert := &store.Certificate{Code: code, UserID: userID, CourseID: courseID}
	if err := a.store.IssueCertificate(ctx, cert); err != nil {
		return nil, false, err
	}
	return cert, true, nil
}

// issueCertificateForCompletion is the grading auto-issue path. Called
// after a submission is graded; when the user now has every
// required-pass task for this course passed, mints the cert (once) and
// fires the "your certificate is ready" email.
func (a *API) issueCertificateForCompletion(ctx context.Context, userID, courseID int64) {
	required, passed, err := a.store.CountRequiredTasksPassed(ctx, userID, courseID)
	if err != nil || required == 0 || passed < required {
		return
	}
	cert, created, err := a.ensureCertificate(ctx, userID, courseID)
	if err != nil {
		log.Printf("certificate auto-issue failed user=%d course=%d: %v", userID, courseID, err)
		return
	}
	if created {
		a.sendCertificateEmailAsync(userID, courseID, cert)
	}
}

// issueAdminCertificate is the manual path admins use to issue a cert
// directly from the course screen / submissions inbox.
func (a *API) issueAdminCertificate(w http.ResponseWriter, r *http.Request) {
	courseID := parseID(chi.URLParam(r, "id"))
	var in struct {
		UserID int64 `json:"userId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.UserID <= 0 {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}
	cert, created, err := a.ensureCertificate(r.Context(), in.UserID, courseID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if created {
		a.sendCertificateEmailAsync(in.UserID, courseID, cert)
		writeJSON(w, http.StatusCreated, cert)
		return
	}
	writeJSON(w, http.StatusOK, cert)
}

// completeCourse is the student-driven auto-issue path. The browser
// calls it once the learner has finished every lesson (lesson progress
// lives client-side), and the server validates eligibility before
// minting: the caller must have access to the course, and any
// required-pass tasks must already be passed. Idempotent — returns the
// existing certificate on repeat calls.
func (a *API) completeCourse(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	slug := chi.URLParam(r, "slug")
	course, err := a.store.GetCourseBySlug(r.Context(), slug, true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "course not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	canAccess, err := a.store.UserCanAccessCourse(r.Context(), uid, course.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !canAccess {
		writeError(w, http.StatusForbidden, "you don't have access to this course")
		return
	}
	// Gate on graded work: if the course has required-pass tasks, every
	// one must be passed before a certificate can be issued.
	required, passed, err := a.store.CountRequiredTasksPassed(r.Context(), uid, course.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if required > 0 && passed < required {
		writeError(w, http.StatusConflict,
			"finish and pass the required assignments to earn your certificate")
		return
	}
	cert, created, err := a.ensureCertificate(r.Context(), uid, course.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if created {
		a.sendCertificateEmailAsync(uid, course.ID, cert)
		writeJSON(w, http.StatusCreated, cert)
		return
	}
	writeJSON(w, http.StatusOK, cert)
}

// listMyCertificates — used by /account/courses to badge completed
// courses with a 🎓 link.
func (a *API) listMyCertificates(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	items, err := a.store.ListUserCertificatesDetailed(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// getPublicCertificate verifies a cert by its public code. Anyone
// with the link gets the student name, course title, and date — used
// by the share-friendly verify page.
func (a *API) getPublicCertificate(w http.ResponseWriter, r *http.Request) {
	v, err := a.store.GetCertificateByCode(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		writeError(w, http.StatusNotFound, "certificate not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// downloadCertificate renders the certificate as a PDF and streams it.
func (a *API) downloadCertificate(w http.ResponseWriter, r *http.Request) {
	v, err := a.store.GetCertificateByCode(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		writeError(w, http.StatusNotFound, "certificate not found")
		return
	}
	pdf := renderCertificatePDF(v, a.cfg.PublicBaseURL)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="kuza-kizazi-certificate-`+v.Code+`.pdf"`)
	if err := pdf.Output(w); err != nil {
		log.Printf("certificate pdf render failed for %s: %v", v.Code, err)
	}
}

// Brand palette (RGB) reused across the certificate.
var (
	certOrange = [3]int{0xef, 0x5a, 0x28}
	certInk    = [3]int{0x18, 0x18, 0x1b}
	certMuted  = [3]int{0x6b, 0x6b, 0x72}
)

// renderCertificatePDF lays out a one-page A4 landscape certificate:
// a double brand frame, centred credential copy, a drawn verification
// seal, a signature line, and a scannable QR linking to the public
// verify page. Sticks to fpdf built-in fonts to keep the binary
// self-contained.
func renderCertificatePDF(v *store.CertificateView, publicBaseURL string) *fpdf.Fpdf {
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(20, 20, 20)
	pdf.SetAutoPageBreak(false, 0)
	pdf.AddPage()

	const pageW = 297.0
	verifyURL := strings.TrimRight(publicBaseURL, "/") + "/cert/" + v.Code

	setDraw := func(c [3]int) { pdf.SetDrawColor(c[0], c[1], c[2]) }
	setText := func(c [3]int) { pdf.SetTextColor(c[0], c[1], c[2]) }
	centered := func(y float64, h float64, txt string) {
		pdf.SetXY(20, y)
		pdf.CellFormat(0, h, txt, "", 1, "C", false, 0, "")
	}

	// Double decorative frame.
	setDraw(certOrange)
	pdf.SetLineWidth(1.4)
	pdf.Rect(10, 10, pageW-20, 190, "D")
	setDraw(certInk)
	pdf.SetLineWidth(0.3)
	pdf.Rect(13, 13, pageW-26, 184, "D")

	// Brand mark (centred square) + wordmark.
	pdf.SetFillColor(certOrange[0], certOrange[1], certOrange[2])
	pdf.Rect(pageW/2-3, 22, 6, 6, "F")
	setText(certInk)
	pdf.SetFont("Helvetica", "B", 13)
	centered(30, 7, "KUZA KIZAZI")

	// Eyebrow.
	setText(certOrange)
	pdf.SetFont("Helvetica", "B", 12)
	centered(48, 7, "CERTIFICATE OF COMPLETION")

	// Credential body.
	setText(certMuted)
	pdf.SetFont("Helvetica", "", 12)
	centered(66, 7, "This certifies that")

	setText(certInk)
	pdf.SetFont("Helvetica", "B", 34)
	centered(76, 16, v.StudentName)

	setText(certMuted)
	pdf.SetFont("Helvetica", "", 13)
	centered(100, 7, "has successfully completed")

	setText(certInk)
	pdf.SetFont("Helvetica", "B", 21)
	centered(110, 11, v.CourseTitle)

	setText(certMuted)
	pdf.SetFont("Helvetica", "", 12)
	centered(128, 7, "Issued on "+v.IssuedAt.Format("2 January 2006"))

	// Verification seal (bottom-left): concentric circles + KK monogram.
	sealX, sealY := 52.0, 165.0
	setDraw(certOrange)
	pdf.SetLineWidth(1.0)
	pdf.Circle(sealX, sealY, 16, "D")
	pdf.SetLineWidth(0.3)
	pdf.Circle(sealX, sealY, 13, "D")
	setText(certOrange)
	pdf.SetFont("Helvetica", "B", 17)
	pdf.SetXY(sealX-16, sealY-7)
	pdf.CellFormat(32, 8, "KK", "", 0, "C", false, 0, "")
	setText(certMuted)
	pdf.SetFont("Helvetica", "B", 6)
	pdf.SetXY(sealX-16, sealY+2)
	pdf.CellFormat(32, 4, "VERIFIED", "", 0, "C", false, 0, "")

	// Signature line (centre).
	setDraw(certInk)
	pdf.SetLineWidth(0.4)
	pdf.Line(120, 172, 177, 172)
	setText(certInk)
	pdf.SetFont("Helvetica", "", 11)
	pdf.SetXY(118, 174)
	pdf.CellFormat(61, 5, "Kuza Kizazi", "", 0, "C", false, 0, "")
	setText(certMuted)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetXY(118, 179)
	pdf.CellFormat(61, 4, "Director", "", 0, "C", false, 0, "")

	// QR (bottom-right) linking to the verify page.
	if png, err := qrcode.Encode(verifyURL, qrcode.Medium, 512); err == nil {
		opt := fpdf.ImageOptions{ImageType: "PNG", ReadDpi: false}
		name := "qr-" + v.Code
		pdf.RegisterImageOptionsReader(name, opt, bytes.NewReader(png))
		pdf.ImageOptions(name, 234, 150, 28, 28, false, opt, 0, "")
		setText(certMuted)
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetXY(223, 179)
		pdf.CellFormat(50, 4, "Scan to verify", "", 0, "C", false, 0, "")
	}

	// Footer: certificate id + verify URL (centred along the bottom).
	setText(certMuted)
	pdf.SetFont("Helvetica", "", 8)
	centered(188, 4, "Certificate ID: "+v.Code)
	centered(192, 4, "Verify at "+verifyURL)

	return pdf
}

// sendCertificateEmailAsync emails the student a "your certificate
// is ready" notice with verify + download URLs. Best-effort — SMTP
// failures are logged but never block the issuing path.
func (a *API) sendCertificateEmailAsync(userID, courseID int64, cert *store.Certificate) {
	if a.mailer == nil {
		return
	}
	ctx := context.Background()
	user, err := a.store.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return
	}
	course, err := a.store.GetCourseByID(ctx, courseID)
	if err != nil || course == nil {
		return
	}
	siteBase := strings.TrimRight(a.cfg.PublicBaseURL, "/")
	apiBase := strings.TrimRight(a.cfg.APIPublicURL, "/")
	verifyURL := siteBase + "/cert/" + cert.Code
	downloadURL := apiBase + "/api/cert/" + cert.Code + "/download"

	subject := "Your certificate is ready — " + course.Title
	greeting := "Hi " + strings.TrimSpace(user.Name)
	if strings.TrimSpace(user.Name) == "" {
		greeting = "Hello"
	}
	htmlBody := `<!doctype html><html><body style="font-family:system-ui,sans-serif;color:#222;line-height:1.6;max-width:640px;margin:0 auto;padding:0 16px">` +
		`<p>` + greeting + `,</p>` +
		`<p>Congratulations — you've completed <strong>` + course.Title + `</strong>. Your certificate is ready to download.</p>` +
		`<p><a href="` + downloadURL + `" style="display:inline-block;background:#ef5a28;color:#fff;text-decoration:none;font-weight:600;padding:10px 18px;border-radius:999px">Download certificate (PDF)</a></p>` +
		`<p>Share or verify it at: <a href="` + verifyURL + `">` + verifyURL + `</a></p>` +
		`<p style="color:#888;font-size:13px;margin-top:24px">Certificate ID: ` + cert.Code + `</p>` +
		`</body></html>`
	textBody := greeting + ",\n\nCongratulations — you've completed " + course.Title + ". Your certificate is ready.\n\nDownload: " + downloadURL + "\nVerify: " + verifyURL + "\n\nCertificate ID: " + cert.Code

	// Use the SMTPMailer's lower-level send directly — the Mailer
	// interface doesn't (yet) have a dedicated SendCertificate.
	if s, ok := a.mailer.(*SMTPMailer); ok {
		go func() {
			if err := s.send(user.Email, subject, textBody, htmlBody); err != nil {
				log.Printf("certificate email failed for %s: %v", user.Email, err)
			}
		}()
	}
}
