package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

// listAdminNewsletters returns every newsletter (drafts + sent).
func (a *API) listAdminNewsletters(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListNewsletters(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// listSubscriberTagStats powers the audience-size hint in the composer.
func (a *API) listSubscriberTagStats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.store.SubscriberTagStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Always include a synthetic "all" row so the UI can show the
	// total-list size alongside per-tag counts.
	all, _ := a.store.AudienceForTags(r.Context(), nil, true)
	resp := map[string]any{
		"tags":  stats,
		"total": len(all),
	}
	writeJSON(w, http.StatusOK, resp)
}

type newsletterInput struct {
	Subject      string   `json:"subject"`
	Body         string   `json:"body"`
	AudienceAll  bool     `json:"audienceAll"`
	AudienceTags []string `json:"audienceTags"`
}

func (a *API) createAdminNewsletter(w http.ResponseWriter, r *http.Request) {
	var in newsletterInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(in.Subject) == "" {
		writeError(w, http.StatusBadRequest, "subject is required")
		return
	}
	uid := currentUserID(r)
	n := &store.Newsletter{
		Subject:      strings.TrimSpace(in.Subject),
		Body:         in.Body,
		AudienceAll:  in.AudienceAll,
		AudienceTags: in.AudienceTags,
		CreatedBy:    &uid,
	}
	if err := a.store.CreateNewsletter(r.Context(), n); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (a *API) getAdminNewsletter(w http.ResponseWriter, r *http.Request) {
	n, err := a.store.GetNewsletterByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "newsletter not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (a *API) updateAdminNewsletter(w http.ResponseWriter, r *http.Request) {
	existing, err := a.store.GetNewsletterByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "newsletter not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing.Status == "sent" {
		writeError(w, http.StatusConflict, "sent newsletters cannot be edited")
		return
	}
	var in newsletterInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(in.Subject) == "" {
		writeError(w, http.StatusBadRequest, "subject is required")
		return
	}
	existing.Subject = strings.TrimSpace(in.Subject)
	existing.Body = in.Body
	existing.AudienceAll = in.AudienceAll
	existing.AudienceTags = in.AudienceTags
	if err := a.store.UpdateNewsletter(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (a *API) deleteAdminNewsletter(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteNewsletter(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "newsletter not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sendAdminNewsletter resolves the audience, fires off one email per
// recipient (with their personal unsubscribe link), and marks the
// newsletter sent. Returns the count actually delivered.
func (a *API) sendAdminNewsletter(w http.ResponseWriter, r *http.Request) {
	n, err := a.store.GetNewsletterByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "newsletter not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n.Status == "sent" {
		writeError(w, http.StatusConflict, "this newsletter has already been sent")
		return
	}
	if a.mailer == nil {
		writeError(w, http.StatusServiceUnavailable,
			"email is not configured — set SMTP_HOST/PORT/USER/PASS/FROM in env")
		return
	}

	audience, err := a.store.AudienceForTags(r.Context(), n.AudienceTags, n.AudienceAll)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(audience) == 0 {
		writeError(w, http.StatusBadRequest, "no subscribers match the chosen audience")
		return
	}

	// Send concurrently with a small worker pool so a slow SMTP doesn't
	// hold the HTTP handler open for minutes on a big list.
	sent := sendNewsletter(r.Context(), a.mailer, a.cfg.PublicBaseURL, n, audience)
	if err := a.store.MarkNewsletterSent(r.Context(), n.ID, sent); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n.Status = "sent"
	n.SentCount = sent
	now := time.Now().UTC()
	n.SentAt = &now
	writeJSON(w, http.StatusOK, n)
}

// sendNewsletter dispatches to a fixed pool of workers and returns the
// number of recipients delivered to without error. Failures are logged.
func sendNewsletter(ctx context.Context, m Mailer, baseURL string, n *store.Newsletter, audience []store.NewsletterSubscriber) int {
	const workers = 5
	jobs := make(chan store.NewsletterSubscriber)
	var wg sync.WaitGroup
	var mu sync.Mutex
	sent := 0

	base := strings.TrimRight(baseURL, "/")

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				unsubURL := base + "/unsubscribe/" + url.PathEscape(s.UnsubscribeToken)
				if err := m.SendNewsletter(ctx, s.Email, s.Name, n.Subject, n.Body, unsubURL); err != nil {
					log.Printf("newsletter %d → %s failed: %v", n.ID, s.Email, err)
					continue
				}
				mu.Lock()
				sent++
				mu.Unlock()
			}
		}()
	}
	for _, s := range audience {
		jobs <- s
	}
	close(jobs)
	wg.Wait()
	return sent
}

// --- Public unsubscribe ---

// getUnsubscribePublic returns whether the token is still valid + which
// email it belongs to, so the public page can show a confirmation.
func (a *API) getUnsubscribePublic(w http.ResponseWriter, r *http.Request) {
	sub, err := a.store.GetSubscriberByToken(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "this unsubscribe link is not valid")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	already := sub.UnsubscribedAt != nil
	writeJSON(w, http.StatusOK, map[string]any{
		"email":            sub.Email,
		"alreadyUnsubbed":  already,
	})
}

// acceptUnsubscribePublic flips the unsubscribed flag. Idempotent — a
// repeat call is fine, the subscriber just stays unsubscribed.
func (a *API) acceptUnsubscribePublic(w http.ResponseWriter, r *http.Request) {
	sub, err := a.store.GetSubscriberByToken(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "this unsubscribe link is not valid")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sub.UnsubscribedAt == nil {
		if err := a.store.MarkUnsubscribed(r.Context(), sub.ID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"email": sub.Email})
}
