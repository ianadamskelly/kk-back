package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-pdf/fpdf"

	"kuzakizazi/internal/store"
)

// mintCertificateCode returns a short opaque identifier like
// "KK-AB12CD34" — printable on the cert and usable as the URL slug.
func mintCertificateCode() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "KK-" + strings.ToUpper(hex.EncodeToString(buf)), nil
}

// issueCertificateForCompletion is the auto-issue path. Called after
// a submission is graded; when the user now has every required-pass
// task for this course passed AND no cert exists yet, mints one and
// fires the "your certificate is ready" email.
func (a *API) issueCertificateForCompletion(ctx context.Context, userID, courseID int64) {
	required, passed, err := a.store.CountRequiredTasksPassed(ctx, userID, courseID)
	if err != nil || required == 0 || passed < required {
		return
	}
	if existing, _ := a.store.FindCertificate(ctx, userID, courseID); existing != nil {
		return
	}
	code, err := mintCertificateCode()
	if err != nil {
		return
	}
	cert := &store.Certificate{Code: code, UserID: userID, CourseID: courseID}
	if err := a.store.IssueCertificate(ctx, cert); err != nil {
		log.Printf("certificate auto-issue failed user=%d course=%d: %v", userID, courseID, err)
		return
	}
	a.sendCertificateEmailAsync(userID, courseID, cert)
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
	if existing, _ := a.store.FindCertificate(r.Context(), in.UserID, courseID); existing != nil {
		writeJSON(w, http.StatusOK, existing)
		return
	}
	code, err := mintCertificateCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cert := &store.Certificate{Code: code, UserID: in.UserID, CourseID: courseID}
	if err := a.store.IssueCertificate(r.Context(), cert); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.sendCertificateEmailAsync(in.UserID, courseID, cert)
	writeJSON(w, http.StatusCreated, cert)
}

// listMyCertificates — used by /account/courses to badge completed
// courses with a 🎓 link.
func (a *API) listMyCertificates(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	items, err := a.store.ListUserCertificates(r.Context(), uid)
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

// renderCertificatePDF lays out a one-page A4 landscape certificate
// with the brand mark, the student name in big type, and the verify
// URL + cert ID in the footer. Sticks to fpdf built-in fonts to keep
// the binary self-contained.
func renderCertificatePDF(v *store.CertificateView, publicBaseURL string) *fpdf.Fpdf {
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(20, 20, 20)
	pdf.AddPage()

	// Brand mark — orange square in the top-left.
	pdf.SetFillColor(0xef, 0x5a, 0x28)
	pdf.Rect(20, 18, 6, 6, "F")
	pdf.SetTextColor(0x18, 0x18, 0x1b)
	pdf.SetFont("Helvetica", "B", 14)
	pdf.SetXY(30, 18)
	pdf.CellFormat(0, 6, "Kuza Kizazi", "", 0, "L", false, 0, "")

	// Eyebrow + headline.
	pdf.SetTextColor(0xef, 0x5a, 0x28)
	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetXY(20, 60)
	pdf.CellFormat(0, 6, "CERTIFICATE OF COMPLETION", "", 1, "C", false, 0, "")

	pdf.SetTextColor(0x18, 0x18, 0x1b)
	pdf.SetFont("Helvetica", "B", 36)
	pdf.SetXY(20, 80)
	pdf.CellFormat(0, 16, v.StudentName, "", 1, "C", false, 0, "")

	pdf.SetFont("Helvetica", "", 14)
	pdf.SetXY(20, 110)
	pdf.CellFormat(0, 8, "has successfully completed", "", 1, "C", false, 0, "")

	pdf.SetFont("Helvetica", "B", 20)
	pdf.SetXY(20, 124)
	pdf.CellFormat(0, 10, v.CourseTitle, "", 1, "C", false, 0, "")

	pdf.SetFont("Helvetica", "", 12)
	pdf.SetXY(20, 142)
	pdf.CellFormat(0, 8, fmt.Sprintf("Issued on %s", v.IssuedAt.Format("2 January 2006")), "", 1, "C", false, 0, "")

	// Signature line on the right.
	pdf.SetDrawColor(0x18, 0x18, 0x1b)
	pdf.SetLineWidth(0.4)
	pdf.Line(190, 175, 270, 175)
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetXY(190, 178)
	pdf.CellFormat(80, 5, "Kuza Kizazi", "", 1, "C", false, 0, "")
	pdf.SetXY(190, 183)
	pdf.CellFormat(80, 4, "Director", "", 1, "C", false, 0, "")

	// Footer: certificate id + verify URL.
	verifyURL := strings.TrimRight(publicBaseURL, "/") + "/cert/" + v.Code
	pdf.SetTextColor(0x55, 0x55, 0x55)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetXY(20, 195)
	pdf.CellFormat(0, 5, "Certificate ID: "+v.Code, "", 1, "L", false, 0, "")
	pdf.SetXY(20, 200)
	pdf.CellFormat(0, 5, "Verify at: "+verifyURL, "", 1, "L", false, 0, "")

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

// Silence "imported and not used" for errors when this file's only
// consumer is an external caller (gradeSubmission via store.ErrNotFound).
var _ = errors.New
