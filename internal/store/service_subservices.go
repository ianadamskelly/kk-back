package store

import (
	"context"
	"time"
)

// ServiceSubservice is a concrete capability displayed within a parent service.
type ServiceSubservice struct {
	ID        int64     `json:"id" db:"id"`
	ServiceID int64     `json:"serviceId" db:"service_id"`
	Title     string    `json:"title" db:"title"`
	Summary   string    `json:"summary" db:"summary"`
	Body      string    `json:"body" db:"body"`
	SortOrder int       `json:"sortOrder" db:"sort_order"`
	Status    string    `json:"status" db:"status"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}

const serviceSubserviceSelect = `SELECT id, service_id, title, summary, body, sort_order, status, created_at, updated_at FROM service_subservices`

func (s *Store) ListServiceSubservices(ctx context.Context, serviceID int64, publishedOnly bool) ([]ServiceSubservice, error) {
	q := serviceSubserviceSelect + ` WHERE service_id = $1`
	if publishedOnly {
		q += ` AND status = 'published'`
	}
	q += ` ORDER BY sort_order, title`
	return queryRows[ServiceSubservice](ctx, s.pool, q, serviceID)
}

func (s *Store) GetServiceSubservice(ctx context.Context, serviceID, id int64) (*ServiceSubservice, error) {
	return queryOne[ServiceSubservice](ctx, s.pool, serviceSubserviceSelect+` WHERE service_id = $1 AND id = $2`, serviceID, id)
}

func (s *Store) CreateServiceSubservice(ctx context.Context, v *ServiceSubservice) error {
	if v.Status == "" {
		v.Status = "published"
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO service_subservices (service_id, title, summary, body, sort_order, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at`,
		v.ServiceID, v.Title, v.Summary, v.Body, v.SortOrder, v.Status,
	).Scan(&v.ID, &v.CreatedAt, &v.UpdatedAt)
}

func (s *Store) UpdateServiceSubservice(ctx context.Context, v *ServiceSubservice) error {
	if v.Status == "" {
		v.Status = "published"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE service_subservices
		SET title=$1, summary=$2, body=$3, sort_order=$4, status=$5, updated_at=now()
		WHERE service_id=$6 AND id=$7`,
		v.Title, v.Summary, v.Body, v.SortOrder, v.Status, v.ServiceID, v.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteServiceSubservice(ctx context.Context, serviceID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM service_subservices WHERE service_id = $1 AND id = $2`, serviceID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
