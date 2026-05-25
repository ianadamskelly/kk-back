package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// NewsletterSubscriber is one row of the mailing list. Tags are used to
// target newsletters at slices of the audience (e.g. only people who
// signed up while buying a course).
type NewsletterSubscriber struct {
	ID                int64      `json:"id" db:"id"`
	Email             string     `json:"email" db:"email"`
	Name              string     `json:"name" db:"name"`
	Tags              []string   `json:"tags" db:"tags"`
	Source            string     `json:"source" db:"source"`
	UserID            *int64     `json:"userId" db:"user_id"`
	UnsubscribeToken  string     `json:"-" db:"unsubscribe_token"`
	UnsubscribedAt    *time.Time `json:"unsubscribedAt" db:"unsubscribed_at"`
	CreatedAt         time.Time  `json:"createdAt" db:"created_at"`
}

const subscriberSelect = `SELECT id, email, name, tags, source, user_id, unsubscribe_token, unsubscribed_at, created_at FROM newsletter_subscribers`

// SubscriberUpsert holds the fields a caller can set when adding or
// updating a subscriber.
type SubscriberUpsert struct {
	Email  string
	Name   string
	Source string
	UserID *int64
	// Tags to add (existing tags are preserved). The 'signup' tag is
	// added implicitly when Source != "newsletter" because every account
	// holder is by definition a signed-up user.
	Tags []string
}

// UpsertSubscriberWithTags inserts a new subscriber or merges new tags +
// optional name/user_id onto an existing one. Subscribers who previously
// unsubscribed are NOT silently re-subscribed — they keep unsubscribed_at
// set and won't appear in audiences.
func (s *Store) UpsertSubscriberWithTags(ctx context.Context, in SubscriberUpsert) (*NewsletterSubscriber, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, ErrNotFound // caller treats this as "skip"
	}
	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = "website"
	}

	// Dedupe + normalise tag list, plus always include the source tag.
	wantTags := normaliseTags(append([]string{source}, in.Tags...))

	token, err := newUnsubToken()
	if err != nil {
		return nil, err
	}

	row, err := queryOne[NewsletterSubscriber](ctx, s.pool, `
		INSERT INTO newsletter_subscribers (email, name, tags, source, user_id, unsubscribe_token)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (email) DO UPDATE
		   SET name              = CASE WHEN newsletter_subscribers.name = '' THEN EXCLUDED.name ELSE newsletter_subscribers.name END,
		       user_id           = COALESCE(newsletter_subscribers.user_id, EXCLUDED.user_id),
		       unsubscribe_token = COALESCE(newsletter_subscribers.unsubscribe_token, EXCLUDED.unsubscribe_token),
		       tags              = (
		           SELECT array_agg(DISTINCT t)
		           FROM unnest(newsletter_subscribers.tags || EXCLUDED.tags) AS t
		       )
		RETURNING `+subscriberColumns(),
		email, in.Name, wantTags, source, in.UserID, token,
	)
	if err != nil {
		return nil, err
	}
	return row, nil
}

// AddSubscriber is the legacy single-arg helper used by the public
// /api/newsletter form. Defaults to the "newsletter" source + tag.
func (s *Store) AddSubscriber(ctx context.Context, email string) error {
	_, err := s.UpsertSubscriberWithTags(ctx, SubscriberUpsert{
		Email:  email,
		Source: "newsletter",
	})
	return err
}

// ListSubscribers returns every subscriber, newest first.
func (s *Store) ListSubscribers(ctx context.Context) ([]NewsletterSubscriber, error) {
	return queryRows[NewsletterSubscriber](ctx, s.pool,
		subscriberSelect+` ORDER BY created_at DESC, id DESC`)
}

// DeleteSubscriber removes a subscriber by id (hard delete; for soft
// opt-out use the unsubscribe token).
func (s *Store) DeleteSubscriber(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM newsletter_subscribers WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetSubscriberByToken looks up an active subscriber by their unsubscribe
// token. Returns ErrNotFound if the token is unknown OR already used.
func (s *Store) GetSubscriberByToken(ctx context.Context, token string) (*NewsletterSubscriber, error) {
	return queryOne[NewsletterSubscriber](ctx, s.pool,
		subscriberSelect+` WHERE unsubscribe_token = $1`, token)
}

// MarkUnsubscribed stamps unsubscribed_at; the subscriber row stays in
// the DB so we don't accidentally re-add them on the next signup.
func (s *Store) MarkUnsubscribed(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE newsletter_subscribers SET unsubscribed_at = now() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AudienceForTags returns every active (not-unsubscribed) subscriber that
// matches the audience filter. tags == nil + all == true → everyone;
// tags non-empty + all == false → anyone holding ANY of the listed tags.
func (s *Store) AudienceForTags(ctx context.Context, tags []string, all bool) ([]NewsletterSubscriber, error) {
	if all {
		return queryRows[NewsletterSubscriber](ctx, s.pool,
			subscriberSelect+` WHERE unsubscribed_at IS NULL ORDER BY id`)
	}
	if len(tags) == 0 {
		return []NewsletterSubscriber{}, nil
	}
	return queryRows[NewsletterSubscriber](ctx, s.pool,
		subscriberSelect+` WHERE unsubscribed_at IS NULL AND tags && $1 ORDER BY id`,
		normaliseTags(tags))
}

// SubscriberTagStats returns tag → active subscriber count, used by the
// admin newsletter composer to show the audience size as the user picks
// tags.
type SubscriberTagStat struct {
	Tag   string `json:"tag" db:"tag"`
	Count int    `json:"count" db:"count"`
}

func (s *Store) SubscriberTagStats(ctx context.Context) ([]SubscriberTagStat, error) {
	return queryRows[SubscriberTagStat](ctx, s.pool, `
		SELECT tag, COUNT(*)::int AS count
		FROM newsletter_subscribers, LATERAL unnest(tags) AS tag
		WHERE unsubscribed_at IS NULL
		GROUP BY tag
		ORDER BY count DESC, tag`)
}

func subscriberColumns() string {
	return "id, email, name, tags, source, user_id, unsubscribe_token, unsubscribed_at, created_at"
}

// normaliseTags lower-cases, trims, dedupes, and drops empty entries.
// Sorted for stable equality checks.
func normaliseTags(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func newUnsubToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
