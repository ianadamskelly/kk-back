package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"kuzakizazi/internal/store"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
func formatCents(c int64) string {
	return fmt.Sprintf("KSh %d", c/100)
}

// --- Profile ---

// getMyProfile returns the editable profile fields for the signed-in user.
func (a *API) getMyProfile(w http.ResponseWriter, r *http.Request) {
	u, err := a.store.GetUserByID(r.Context(), currentUserID(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// updateMyProfile saves the editable profile fields.
func (a *API) updateMyProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string `json:"name"`
		Phone        string `json:"phone"`
		AddressLine1 string `json:"addressLine1"`
		AddressLine2 string `json:"addressLine2"`
		City         string `json:"city"`
		State        string `json:"state"`
		Country      string `json:"country"`
		PostalCode   string `json:"postalCode"`
		Avatar       string `json:"avatar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := a.store.GetUserByID(r.Context(), currentUserID(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		u.Name = name
	}
	u.Phone = strings.TrimSpace(in.Phone)
	u.AddressLine1 = strings.TrimSpace(in.AddressLine1)
	u.AddressLine2 = strings.TrimSpace(in.AddressLine2)
	u.City = strings.TrimSpace(in.City)
	u.State = strings.TrimSpace(in.State)
	u.Country = strings.TrimSpace(in.Country)
	u.PostalCode = strings.TrimSpace(in.PostalCode)
	u.Avatar = strings.TrimSpace(in.Avatar)
	if err := a.store.UpdateProfile(r.Context(), u); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// changeMyPassword lets the customer rotate their own password. Requires
// the current password as proof — no admin-style bypass here.
func (a *API) changeMyPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(in.New) < 8 {
		writeError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	u, err := a.store.GetUserByID(r.Context(), currentUserID(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Current)) != nil {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.New), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}
	if err := a.store.SetUserPassword(r.Context(), u.ID, string(hash)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Dashboard ---

// getMyDashboard rolls up the figures shown on the customer landing page:
// course count, order count, credit balance, membership status, open
// ticket count, plus a small "recent activity" feed merged from orders +
// tickets so the dashboard always has something to show.
func (a *API) getMyDashboard(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	ctx := r.Context()
	if _, err := a.store.ExpireOverdueMemberships(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	orders, _ := a.store.ListUserOrders(ctx, uid, false)
	tickets, _ := a.store.ListUserTickets(ctx, uid)
	credit, _ := a.store.GetCreditBalance(ctx, uid)
	courses, _ := a.userOwnedCourses(ctx, uid)

	// Membership status.
	status := "guest"
	active := false
	hasCourseAccess := false
	hasLibraryAccess := false
	var periodEnd *time.Time
	membershipPlan := ""
	if m, err := a.store.GetMembership(ctx, uid); err == nil && m != nil {
		periodEnd = &m.CurrentPeriodEnd
		membershipPlan = m.Plan
		if m.Status == "active" && m.CurrentPeriodEnd.After(time.Now().UTC()) {
			status = "member"
			active = true
			hasLibraryAccess = true
			hasCourseAccess = m.Plan == "full"
		} else {
			status = "expired"
		}
	}

	// Open ticket count.
	open := 0
	for _, t := range tickets {
		if t.Status == "open" || t.Status == "replied" {
			open++
		}
	}

	// Recent activity: most recent order + most recent ticket. We
	// hand-build entries so the frontend doesn't have to sort/merge.
	type activity struct {
		Kind      string    `json:"kind"`
		Title     string    `json:"title"`
		Subtitle  string    `json:"subtitle"`
		Href      string    `json:"href"`
		Timestamp time.Time `json:"timestamp"`
	}
	feed := []activity{}
	for i, o := range orders {
		if i >= 3 {
			break
		}
		feed = append(feed, activity{
			Kind:      "order",
			Title:     "Order #" + itoa(o.ID),
			Subtitle:  o.Status + " · " + formatCents(o.TotalCents),
			Href:      "/account/orders",
			Timestamp: o.CreatedAt,
		})
	}
	for i, t := range tickets {
		if i >= 2 {
			break
		}
		feed = append(feed, activity{
			Kind:      "ticket",
			Title:     t.Subject,
			Subtitle:  t.Status + " · " + t.Category,
			Href:      "/account/tickets",
			Timestamp: t.LastReplyAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"stats": map[string]any{
			"coursesCount":     len(courses),
			"ordersCount":      len(orders),
			"openTicketsCount": open,
			"creditCents":      credit,
			"membershipStatus": status,
			"membershipActive": active,
			"membershipPlan":   membershipPlan,
			"hasCourseAccess":  hasCourseAccess,
			"hasLibraryAccess": hasLibraryAccess,
			"periodEnd":        periodEnd,
		},
		"continueLearning": pickContinueLearning(courses),
		"recentActivity":   feed,
	})
}

// --- Downloads + owned courses ---

// listMyDownloads returns the products the user has bought via
// confirmed orders, augmented with signed download URLs for any
// digital files attached. Used to populate the "My Downloads" tab.
func (a *API) listMyDownloads(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	orders, err := a.store.ListUserOrders(r.Context(), uid, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type downloadFile struct {
		DownloadID         int64  `json:"downloadId"`
		URL                string `json:"url"`
		Label              string `json:"label"`
		SizeBytes          int64  `json:"sizeBytes"`
		DownloadsUsed      int    `json:"downloadsUsed"`
		MaxDownloads       *int   `json:"maxDownloads"`
		DownloadsRemaining *int   `json:"downloadsRemaining"`
	}
	type download struct {
		OrderID     int64          `json:"orderId"`
		ProductID   *int64         `json:"productId"`
		ProductName string         `json:"productName"`
		Quantity    int            `json:"quantity"`
		PurchasedAt time.Time      `json:"purchasedAt"`
		Files       []downloadFile `json:"files"`
	}
	items := []download{}
	for _, o := range orders {
		// Only confirmed / fulfilled orders unlock downloads. Pending or
		// cancelled orders shouldn't show up.
		if o.Status != "confirmed" && o.Status != "fulfilled" {
			continue
		}
		if o.Kind != "shop" {
			continue
		}
		// Resolve digital downloads for this order up front so we can
		// merge them into per-product line items.
		orderFiles, err := a.store.ListOrderDigitalDownloads(r.Context(), uid, o.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		byProduct := map[int64][]store.CustomerDownload{}
		for _, f := range orderFiles {
			byProduct[f.ProductID] = append(byProduct[f.ProductID], f)
		}
		for _, it := range o.Items {
			if it.ProductID == nil {
				continue
			}
			files := []downloadFile{}
			for _, f := range byProduct[*it.ProductID] {
				token, err := a.signDownloadToken(uid, o.ID, f.DownloadID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				var remaining *int
				if f.MaxDownloads != nil {
					r := *f.MaxDownloads - f.DownloadsUsed
					if r < 0 {
						r = 0
					}
					remaining = &r
				}
				files = append(files, downloadFile{
					DownloadID:         f.DownloadID,
					URL:                "/api/downloads/" + token,
					Label:              f.Label,
					SizeBytes:          f.SizeBytes,
					DownloadsUsed:      f.DownloadsUsed,
					MaxDownloads:       f.MaxDownloads,
					DownloadsRemaining: remaining,
				})
			}
			items = append(items, download{
				OrderID:     o.ID,
				ProductID:   it.ProductID,
				ProductName: it.ProductName,
				Quantity:    it.Quantity,
				PurchasedAt: o.CreatedAt,
				Files:       files,
			})
		}
	}
	writeJSON(w, http.StatusOK, items)
}

// listMyCourses returns the courses the user has access to. Active
// members get the whole catalogue; everyone else gets what they've
// individually purchased.
func (a *API) listMyCourses(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	items, err := a.userOwnedCourses(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// userOwnedCourses centralises the "what courses can this user see?"
// logic for both the dashboard stats and the /account/courses list.
func (a *API) userOwnedCourses(ctx context.Context, uid int64) ([]store.Course, error) {
	if active, _ := a.store.IsActiveCourseMember(ctx, uid); active {
		return a.store.ListCourses(ctx, true)
	}
	// Owned-by-purchase: walk the user's confirmed orders for course items.
	orders, err := a.store.ListUserOrders(ctx, uid, false)
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	owned := []store.Course{}
	for _, o := range orders {
		if o.Status != "confirmed" && o.Status != "fulfilled" {
			continue
		}
		for _, it := range o.Items {
			if it.CourseID == nil || seen[*it.CourseID] {
				continue
			}
			seen[*it.CourseID] = true
			if c, err := a.store.GetCourseByID(ctx, *it.CourseID); err == nil && c != nil {
				owned = append(owned, *c)
			}
		}
	}
	// Plus any free courses (priceCents=0) — those count as "enrolled" too.
	all, err := a.store.ListCourses(ctx, true)
	if err == nil {
		for _, c := range all {
			if c.PriceCents == 0 && !seen[c.ID] {
				seen[c.ID] = true
				owned = append(owned, c)
			}
		}
	}
	return owned, nil
}

// pickContinueLearning returns the first owned course (if any) for the
// "Ready to start?" card on the dashboard. Future iterations can store
// last-watched lesson per user and surface that here instead.
func pickContinueLearning(owned []store.Course) any {
	if len(owned) == 0 {
		return nil
	}
	c := owned[0]
	return map[string]any{
		"id":         c.ID,
		"slug":       c.Slug,
		"title":      c.Title,
		"coverImage": c.CoverImage,
		"level":      c.Level,
		"duration":   c.Duration,
	}
}

// --- Customer testimonials ---

// listMyTestimonials returns every testimonial the user has submitted.
func (a *API) listMyTestimonials(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	items, err := a.store.ListUserTestimonials(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// createMyTestimonial submits a new testimonial as the signed-in
// customer. It always lands in 'pending' status — an admin reviews
// before it appears on the public site.
func (a *API) createMyTestimonial(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, err := a.store.GetUserByID(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}
	var in struct {
		Role    string `json:"role"`
		Company string `json:"company"`
		Quote   string `json:"quote"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	quote := strings.TrimSpace(in.Quote)
	if len(quote) < 20 {
		writeError(w, http.StatusBadRequest, "please share at least a sentence (20 characters)")
		return
	}
	now := time.Now().UTC()
	t := &store.Testimonial{
		Author:      user.Name,
		Role:        strings.TrimSpace(in.Role),
		Company:     strings.TrimSpace(in.Company),
		Quote:       quote,
		Status:      "pending",
		UserID:      &uid,
		Source:      "customer",
		SubmittedAt: &now,
	}
	if err := a.store.CreateTestimonial(r.Context(), t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// --- Tickets (customer side) ---

// listMyTickets returns the signed-in user's tickets.
func (a *API) listMyTickets(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	items, err := a.store.ListUserTickets(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// createMyTicket opens a new support ticket as the signed-in customer.
func (a *API) createMyTicket(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, err := a.store.GetUserByID(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}
	var in struct {
		Subject  string `json:"subject"`
		Category string `json:"category"`
		Body     string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.Subject = strings.TrimSpace(in.Subject)
	in.Body = strings.TrimSpace(in.Body)
	if in.Subject == "" || in.Body == "" {
		writeError(w, http.StatusBadRequest, "subject and message are required")
		return
	}
	t := &store.Ticket{
		UserID:   uid,
		Subject:  in.Subject,
		Category: in.Category,
		Status:   "open",
	}
	if err := a.store.CreateTicket(r.Context(), t, in.Body, user.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// getMyTicket returns one of the signed-in user's tickets with its full
// message thread.
func (a *API) getMyTicket(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id := parseID(chi.URLParam(r, "id"))
	t, err := a.store.GetTicket(r.Context(), id, uid)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "ticket not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	msgs, _ := a.store.ListTicketMessages(r.Context(), t.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":   t,
		"messages": msgs,
	})
}

// replyToMyTicket lets the customer post a reply on their own ticket.
func (a *API) replyToMyTicket(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, err := a.store.GetUserByID(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "account not found")
		return
	}
	id := parseID(chi.URLParam(r, "id"))
	if _, err := a.store.GetTicket(r.Context(), id, uid); err != nil {
		writeError(w, http.StatusNotFound, "ticket not found")
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "message body is required")
		return
	}
	authorID := uid
	m := &store.TicketMessage{
		TicketID:   id,
		AuthorID:   &authorID,
		AuthorRole: "customer",
		AuthorName: user.Name,
		Body:       body,
	}
	if err := a.store.AddTicketMessage(r.Context(), m); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// closeMyTicket lets the customer mark their own ticket as resolved.
func (a *API) closeMyTicket(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id := parseID(chi.URLParam(r, "id"))
	if _, err := a.store.GetTicket(r.Context(), id, uid); err != nil {
		writeError(w, http.StatusNotFound, "ticket not found")
		return
	}
	if err := a.store.SetTicketStatus(r.Context(), id, "closed"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
