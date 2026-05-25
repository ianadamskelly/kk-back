package store

import (
	"context"
	"crypto/rand"
	"strings"
	"time"
)

// Referee is one row in the "people you've referred" list shown on the
// account page.
type Referee struct {
	UserID     int64     `json:"userId" db:"user_id"`
	Name       string    `json:"name" db:"name"`
	Email      string    `json:"email" db:"email"`
	JoinedAt   time.Time `json:"joinedAt" db:"joined_at"`
	Rewarded   bool      `json:"rewarded" db:"rewarded"`
	RewardedAt *time.Time `json:"rewardedAt" db:"rewarded_at"`
}

// GetOrCreateReferralCode returns the user's referral code, generating one
// the first time it's requested. Codes are 8 chars from a friendly
// alphabet (no 0/O/1/I confusion).
func (s *Store) GetOrCreateReferralCode(ctx context.Context, userID int64) (string, error) {
	var code *string
	if err := s.pool.QueryRow(ctx,
		`SELECT referral_code FROM users WHERE id = $1`, userID).Scan(&code); err != nil {
		return "", err
	}
	if code != nil && *code != "" {
		return *code, nil
	}
	// Generate and persist a code. Retry on the (extremely unlikely)
	// collision so the unique index doesn't bubble up to the caller.
	for attempt := 0; attempt < 5; attempt++ {
		c := newReferralCode()
		tag, err := s.pool.Exec(ctx,
			`UPDATE users SET referral_code = $1 WHERE id = $2 AND (referral_code IS NULL OR referral_code = '')`,
			c, userID)
		if err == nil && tag.RowsAffected() == 1 {
			return c, nil
		}
		// Either someone else won the race, or the code collided — re-check.
		if err := s.pool.QueryRow(ctx,
			`SELECT referral_code FROM users WHERE id = $1`, userID).Scan(&code); err == nil &&
			code != nil && *code != "" {
			return *code, nil
		}
	}
	return "", ErrNotFound
}

// GetUserByReferralCode returns the user that owns the given referral code,
// case-insensitive.
func (s *Store) GetUserByReferralCode(ctx context.Context, code string) (*User, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	return queryOne[User](ctx, s.pool,
		userSelect+` WHERE referral_code = $1`, code)
}

// SetReferrer attaches a referring user to a (newly created) referee, but
// only if the referee doesn't already have one set.
func (s *Store) SetReferrer(ctx context.Context, refereeID, referrerID int64) error {
	if refereeID == referrerID {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET referred_by_user_id = $1
		 WHERE id = $2 AND referred_by_user_id IS NULL`,
		referrerID, refereeID)
	return err
}

// MarkRefereeRewarded stamps the rewarded_at column on a user so we never
// grant the referrer a second reward for the same person.
func (s *Store) MarkRefereeRewarded(ctx context.Context, refereeID int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET referral_rewarded_at = now()
		 WHERE id = $1 AND referral_rewarded_at IS NULL`,
		refereeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListReferees returns everyone the given user has referred, newest first.
func (s *Store) ListReferees(ctx context.Context, referrerID int64) ([]Referee, error) {
	return queryRows[Referee](ctx, s.pool, `
		SELECT id AS user_id, name, email,
			created_at AS joined_at,
			(referral_rewarded_at IS NOT NULL) AS rewarded,
			referral_rewarded_at AS rewarded_at
		FROM users
		WHERE referred_by_user_id = $1
		ORDER BY created_at DESC`, referrerID)
}

// HasFirstPaidOrder returns true once a user has at least one order whose
// status is anything other than 'pending' / 'cancelled' and whose total
// (after discount + credit) is greater than zero. Used to gate the
// referral reward.
func (s *Store) HasFirstPaidOrder(ctx context.Context, userID, excludeOrderID int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM orders
			WHERE user_id = $1
			  AND id <> $2
			  AND status NOT IN ('pending','cancelled')
			  AND total_cents > 0
		)`, userID, excludeOrderID).Scan(&exists)
	return exists, err
}

// ReferralLeaderboardRow is one row of the admin referrals dashboard.
type ReferralLeaderboardRow struct {
	UserID       int64  `json:"userId" db:"user_id"`
	Name         string `json:"name" db:"name"`
	Email        string `json:"email" db:"email"`
	ReferralCode string `json:"referralCode" db:"referral_code"`
	Total        int    `json:"total" db:"total"`
	Rewarded     int    `json:"rewarded" db:"rewarded"`
}

// ReferralLeaderboard returns top referrers (anyone with at least one
// referee), in descending order of referral count.
func (s *Store) ReferralLeaderboard(ctx context.Context, limit int) ([]ReferralLeaderboardRow, error) {
	if limit <= 0 {
		limit = 25
	}
	return queryRows[ReferralLeaderboardRow](ctx, s.pool, `
		SELECT u.id AS user_id, u.name, u.email,
			COALESCE(u.referral_code, '') AS referral_code,
			COUNT(r.id) AS total,
			COUNT(r.id) FILTER (WHERE r.referral_rewarded_at IS NOT NULL) AS rewarded
		FROM users u
		JOIN users r ON r.referred_by_user_id = u.id
		GROUP BY u.id, u.name, u.email, u.referral_code
		ORDER BY total DESC
		LIMIT $1`, limit)
}

// newReferralCode generates a friendly 8-character code from an alphabet
// that excludes visually confusing characters.
func newReferralCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 8)
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	for i, b := range raw {
		buf[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(buf)
}

