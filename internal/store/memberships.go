package store

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Membership is one row per user with a current_period_end. A user is
// "active" when status='active' AND current_period_end > now().
type Membership struct {
	ID               int64      `json:"id" db:"id"`
	UserID           int64      `json:"userId" db:"user_id"`
	Plan             string     `json:"plan" db:"plan"`
	Status           string     `json:"status" db:"status"`
	StartedAt        time.Time  `json:"startedAt" db:"started_at"`
	CurrentPeriodEnd time.Time  `json:"currentPeriodEnd" db:"current_period_end"`
	CancelledAt      *time.Time `json:"cancelledAt" db:"cancelled_at"`
	CreatedAt        time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt        time.Time  `json:"updatedAt" db:"updated_at"`
}

const membershipSelect = `SELECT id, user_id, plan, status, started_at, current_period_end, cancelled_at, created_at, updated_at FROM memberships`

// GetMembership returns the membership row for the given user, or
// ErrNotFound if they have never subscribed.
func (s *Store) GetMembership(ctx context.Context, userID int64) (*Membership, error) {
	return queryOne[Membership](ctx, s.pool,
		membershipSelect+` WHERE user_id = $1`, userID)
}

// ExtendMembership upserts the user's membership, pushing current_period_end
// forward by `duration` from max(now, current_period_end). Called from the
// payment-verify flow after a successful membership payment.
func (s *Store) ExtendMembership(ctx context.Context, userID int64, duration time.Duration, plan string) (*Membership, error) {
	return extendMembershipTx(ctx, s.pool, userID, duration, plan)
}

type membershipExecutor interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func extendMembershipTx(ctx context.Context, q membershipExecutor, userID int64, duration time.Duration, plan string) (*Membership, error) {
	if plan != "library" {
		plan = "full"
	}
	var row Membership
	err := q.QueryRow(ctx, `
		INSERT INTO memberships (user_id, plan, status, started_at, current_period_end)
		VALUES ($1, $3, 'active', now(), now() + make_interval(secs => $2::double precision))
		ON CONFLICT (user_id) DO UPDATE SET
			plan = CASE
				WHEN memberships.status = 'active'
				  AND memberships.current_period_end > now()
				  AND memberships.plan = 'full' THEN 'full'
				WHEN EXCLUDED.plan = 'full' THEN 'full'
				ELSE EXCLUDED.plan
			END,
			status = 'active',
			current_period_end = GREATEST(memberships.current_period_end, now())
				+ make_interval(secs => $2::double precision),
			cancelled_at = NULL,
			updated_at = now()
		RETURNING `+stripSelectPrefix(membershipSelect),
		userID, int64(duration.Seconds()), plan,
	).Scan(
		&row.ID, &row.UserID, &row.Plan, &row.Status, &row.StartedAt,
		&row.CurrentPeriodEnd, &row.CancelledAt, &row.CreatedAt, &row.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// CancelMembership marks the membership as cancelled but keeps the
// current_period_end so the user retains access through what they've paid for.
func (s *Store) CancelMembership(ctx context.Context, userID int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE memberships SET status = 'cancelled', cancelled_at = now(), updated_at = now()
		WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ExpireOverdueMemberships marks active memberships whose paid period has
// ended as expired. Access checks also compare current_period_end to now(),
// but persisting the status keeps the customer portal and admin list honest.
func (s *Store) ExpireOverdueMemberships(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE memberships
		SET status = 'expired', updated_at = now()
		WHERE status = 'active' AND current_period_end <= now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// MembershipListItem augments a membership row with the user's name/email
// for the admin listing.
type MembershipListItem struct {
	Membership
	UserEmail string `json:"userEmail" db:"user_email"`
	UserName  string `json:"userName" db:"user_name"`
	TotalPaid int64  `json:"totalPaidCents" db:"total_paid_cents"`
}

// ListMemberships returns every membership with user info and lifetime
// payment total (sum of successful membership orders).
func (s *Store) ListMemberships(ctx context.Context) ([]MembershipListItem, error) {
	return queryRows[MembershipListItem](ctx, s.pool, `
		SELECT m.id, m.user_id, m.plan, m.status, m.started_at, m.current_period_end,
		       m.cancelled_at, m.created_at, m.updated_at,
		       u.email AS user_email, u.name AS user_name,
		       COALESCE(SUM(CASE WHEN o.status = 'confirmed' THEN o.total_cents ELSE 0 END), 0)
		         AS total_paid_cents
		FROM memberships m
		JOIN users u ON u.id = m.user_id
		LEFT JOIN orders o ON o.user_id = m.user_id AND o.kind = 'membership'
		GROUP BY m.id, u.id
		ORDER BY m.current_period_end DESC`,
	)
}

// IsActiveMember returns true if the user has an active membership whose
// current_period_end is still in the future. Safe to call with userID=0.
func (s *Store) IsActiveMember(ctx context.Context, userID int64) (bool, error) {
	if userID == 0 {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM memberships
			WHERE user_id = $1 AND status = 'active' AND current_period_end > now()
		)`, userID).Scan(&ok)
	return ok, err
}

// IsActiveCourseMember returns true only for active full memberships. Library
// memberships unlock protected library resources but do not grant course access.
func (s *Store) IsActiveCourseMember(ctx context.Context, userID int64) (bool, error) {
	if userID == 0 {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM memberships
			WHERE user_id = $1
			  AND status = 'active'
			  AND plan = 'full'
			  AND current_period_end > now()
		)`, userID).Scan(&ok)
	return ok, err
}

// UserOwnsCourse returns true if the user has paid for the given course via
// a confirmed order. Free courses (priceCents=0) should bypass this check
// at the API layer.
func (s *Store) UserOwnsCourse(ctx context.Context, userID, courseID int64) (bool, error) {
	if userID == 0 {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM order_items oi
			JOIN orders o ON o.id = oi.order_id
			WHERE oi.course_id = $1 AND o.user_id = $2 AND o.status = 'confirmed'
		)`, courseID, userID).Scan(&ok)
	return ok, err
}

// stripSelectPrefix turns "SELECT a,b,c FROM t" into "a,b,c" so we can reuse
// the column list inside a RETURNING clause.
func stripSelectPrefix(s string) string {
	if i := strings.Index(s, "SELECT "); i >= 0 {
		s = s[i+len("SELECT "):]
	}
	if i := strings.Index(s, " FROM"); i >= 0 {
		s = s[:i]
	}
	return s
}
