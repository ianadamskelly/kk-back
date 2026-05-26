package store

import (
	"context"
	"time"
)

// ServiceRevenue is a manually logged service-income entry, used for the
// "income from services" admin bucket since services are invoiced offline.
type ServiceRevenue struct {
	ID          int64     `json:"id" db:"id"`
	ServiceID   *int64    `json:"serviceId" db:"service_id"`
	ServiceName string    `json:"serviceName" db:"service_name"`
	ClientName  string    `json:"clientName" db:"client_name"`
	AmountCents int64     `json:"amountCents" db:"amount_cents"`
	Currency    string    `json:"currency" db:"currency"`
	OccurredAt  time.Time `json:"occurredAt" db:"occurred_at"`
	Note        string    `json:"note" db:"note"`
	CreatedAt   time.Time `json:"createdAt" db:"created_at"`
}

const serviceRevenueSelect = `SELECT id, service_id, service_name, client_name, amount_cents, currency, occurred_at, note, created_at FROM service_revenue`

func (s *Store) ListServiceRevenue(ctx context.Context) ([]ServiceRevenue, error) {
	return queryRows[ServiceRevenue](ctx, s.pool,
		serviceRevenueSelect+` ORDER BY occurred_at DESC, id DESC`)
}

func (s *Store) CreateServiceRevenue(ctx context.Context, r *ServiceRevenue) error {
	if r.Currency == "" {
		r.Currency = "KES"
	}
	if r.OccurredAt.IsZero() {
		r.OccurredAt = time.Now()
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO service_revenue (service_id, service_name, client_name, amount_cents, currency, occurred_at, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`,
		r.ServiceID, r.ServiceName, r.ClientName, r.AmountCents, r.Currency, r.OccurredAt, r.Note,
	).Scan(&r.ID, &r.CreatedAt)
}

func (s *Store) DeleteServiceRevenue(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM service_revenue WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevenueSummary breaks down all-time confirmed-payment revenue by source.
// Online buckets sum confirmed orders by kind. Services come from the manual
// log. All values are returned in cents — frontend formats with KES.
type RevenueSummary struct {
	Shop        int64 `json:"shopCents"`
	Courses     int64 `json:"coursesCents"`
	Memberships int64 `json:"membershipsCents"`
	Services    int64 `json:"servicesCents"`
	Total       int64 `json:"totalCents"`
}

func (s *Store) RevenueSummary(ctx context.Context) (*RevenueSummary, error) {
	out := &RevenueSummary{}
	// 'confirmed' = paid but not yet shipped (or digital, where there's
	// nothing to ship). 'fulfilled' = paid and shipped. Both are paid
	// orders and should count as revenue; only 'pending' (unpaid) and
	// 'cancelled' should be excluded. Previously this filtered to
	// 'confirmed' only, which made shop revenue silently disappear the
	// moment an admin marked an order fulfilled.
	if err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN kind = 'shop'       THEN total_cents END), 0),
			COALESCE(SUM(CASE WHEN kind = 'course'     THEN total_cents END), 0),
			COALESCE(SUM(CASE WHEN kind = 'membership' THEN total_cents END), 0)
		FROM orders WHERE status IN ('confirmed', 'fulfilled')`,
	).Scan(&out.Shop, &out.Courses, &out.Memberships); err != nil {
		return nil, err
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM service_revenue`,
	).Scan(&out.Services); err != nil {
		return nil, err
	}
	out.Total = out.Shop + out.Courses + out.Memberships + out.Services
	return out, nil
}
