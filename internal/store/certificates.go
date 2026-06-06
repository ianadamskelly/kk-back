package store

import (
	"context"
	"time"
)

// Certificate is one course completion proof issued to a user.
type Certificate struct {
	ID        int64     `json:"id" db:"id"`
	Code      string    `json:"code" db:"code"`
	UserID    int64     `json:"userId" db:"user_id"`
	CourseID  int64     `json:"courseId" db:"course_id"`
	IssuedAt  time.Time `json:"issuedAt" db:"issued_at"`
}

// CertificateView is what the public verify endpoint returns: enough
// to render a "Yes this is a real certificate" page without leaking
// internal IDs.
type CertificateView struct {
	Code        string    `json:"code"`
	StudentName string    `json:"studentName"`
	CourseTitle string    `json:"courseTitle"`
	IssuedAt    time.Time `json:"issuedAt"`
}

const certificateSelect = `SELECT id, code, user_id, course_id, issued_at FROM certificates`

// FindCertificate returns the cert for (user, course), or ErrNotFound.
func (s *Store) FindCertificate(ctx context.Context, userID, courseID int64) (*Certificate, error) {
	return queryOne[Certificate](ctx, s.pool,
		certificateSelect+` WHERE user_id = $1 AND course_id = $2`,
		userID, courseID)
}

// ListUserCertificates returns all certificates issued to the user.
func (s *Store) ListUserCertificates(ctx context.Context, userID int64) ([]Certificate, error) {
	return queryRows[Certificate](ctx, s.pool,
		certificateSelect+` WHERE user_id = $1 ORDER BY issued_at DESC`,
		userID)
}

// CertificateListItem is a certificate joined with its course's display
// fields, so the account "My Certificates" view can render title + cover
// without depending on the user's owned-courses list (which excludes
// courses an admin issued a cert for manually).
type CertificateListItem struct {
	Certificate
	CourseTitle string `json:"courseTitle"`
	CourseSlug  string `json:"courseSlug"`
	CourseCover string `json:"courseCover"`
}

// ListUserCertificatesDetailed is ListUserCertificates plus the course
// title/slug/cover for each row.
func (s *Store) ListUserCertificatesDetailed(ctx context.Context, userID int64) ([]CertificateListItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.code, c.user_id, c.course_id, c.issued_at,
		       co.title, co.slug, co.cover_image
		FROM certificates c
		JOIN courses co ON co.id = c.course_id
		WHERE c.user_id = $1
		ORDER BY c.issued_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CertificateListItem{}
	for rows.Next() {
		var it CertificateListItem
		if err := rows.Scan(&it.ID, &it.Code, &it.UserID, &it.CourseID, &it.IssuedAt,
			&it.CourseTitle, &it.CourseSlug, &it.CourseCover); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// IssueCertificate inserts a new row. Caller supplies the random
// `code` — kept opaque so it's hard to guess one off-the-cuff.
// Returns the existing row when (user, course) already has one
// (idempotent — repeat invocations don't change anything).
func (s *Store) IssueCertificate(ctx context.Context, c *Certificate) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO certificates (code, user_id, course_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, course_id) DO UPDATE
		    SET issued_at = certificates.issued_at
		RETURNING id, code, issued_at`,
		c.Code, c.UserID, c.CourseID,
	).Scan(&c.ID, &c.Code, &c.IssuedAt)
}

// GetCertificateByCode resolves a public certificate code to the
// student/course context needed by the verify page.
func (s *Store) GetCertificateByCode(ctx context.Context, code string) (*CertificateView, error) {
	v := &CertificateView{}
	err := s.pool.QueryRow(ctx, `
		SELECT c.code, u.name, co.title, c.issued_at
		FROM certificates c
		JOIN users   u  ON u.id  = c.user_id
		JOIN courses co ON co.id = c.course_id
		WHERE c.code = $1`,
		code).Scan(&v.Code, &v.StudentName, &v.CourseTitle, &v.IssuedAt)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// CountRequiredTasksPassed returns how many required-pass tasks the
// course has, and how many of those have a 'passed' submission from
// the user. Used to decide whether to issue a certificate.
func (s *Store) CountRequiredTasksPassed(ctx context.Context, userID, courseID int64) (required, passed int, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM course_tasks WHERE course_id = $1 AND required_pass = TRUE`,
		courseID).Scan(&required)
	if err != nil {
		return 0, 0, err
	}
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM course_task_submissions s
		JOIN course_tasks t ON t.id = s.task_id
		WHERE t.course_id = $1
		  AND t.required_pass = TRUE
		  AND s.user_id = $2
		  AND s.grade = 'passed'`,
		courseID, userID).Scan(&passed)
	return required, passed, err
}
