package store

import (
	"context"
	"time"
)

// Course is a learning course made up of ordered lessons. The "marketing"
// fields (PromoVideo / Prerequisites / Outcomes) power the public detail
// page and are managed via the LMS course wizard.
type Course struct {
	ID            int64     `json:"id" db:"id"`
	Slug          string    `json:"slug" db:"slug"`
	Title         string    `json:"title" db:"title"`
	Summary       string    `json:"summary" db:"summary"`
	Description   string    `json:"description" db:"description"`
	CoverImage    string    `json:"coverImage" db:"cover_image"`
	Level         string    `json:"level" db:"level"`
	Duration      string    `json:"duration" db:"duration"`
	Instructor    string    `json:"instructor" db:"instructor"`
	Category      string    `json:"category" db:"category"`
	Language      string    `json:"language" db:"language"`
	PromoVideo    string    `json:"promoVideo" db:"promo_video"`
	Prerequisites string    `json:"prerequisites" db:"prerequisites"`
	Outcomes      string    `json:"outcomes" db:"outcomes"`
	PriceCents    int64     `json:"priceCents" db:"price_cents"`
	Status        string    `json:"status" db:"status"`
	SortOrder     int       `json:"sortOrder" db:"sort_order"`
	CreatedAt     time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt     time.Time `json:"updatedAt" db:"updated_at"`
	Lessons       []Lesson  `json:"lessons" db:"-"`
}

// Lesson is a single unit of a course. IsPreview makes the lesson viewable
// by non-buyers as a free sample.
type Lesson struct {
	ID        int64     `json:"id" db:"id"`
	CourseID  int64     `json:"courseId" db:"course_id"`
	Module    string    `json:"module" db:"module"`
	Slug      string    `json:"slug" db:"slug"`
	Title     string    `json:"title" db:"title"`
	Content   string    `json:"content" db:"content"`
	VideoURL  string    `json:"videoUrl" db:"video_url"`
	Duration  string    `json:"duration" db:"duration"`
	IsPreview bool      `json:"isPreview" db:"is_preview"`
	SortOrder int       `json:"sortOrder" db:"sort_order"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}

const courseSelect = `SELECT id, slug, title, summary, description, cover_image, level, duration, instructor, category, language, promo_video, prerequisites, outcomes, price_cents, status, sort_order, created_at, updated_at FROM courses`
const lessonSelect = `SELECT id, course_id, module, slug, title, content, video_url, duration, is_preview, sort_order, created_at, updated_at FROM lessons`

// ListCourses returns courses ordered for display (without their lessons).
func (s *Store) ListCourses(ctx context.Context, publishedOnly bool) ([]Course, error) {
	q := courseSelect
	if publishedOnly {
		q += ` WHERE status = 'published'`
	}
	q += ` ORDER BY sort_order, created_at DESC`
	return queryRows[Course](ctx, s.pool, q)
}

// GetCourseByID returns one course with its lessons.
func (s *Store) GetCourseByID(ctx context.Context, id int64) (*Course, error) {
	course, err := queryOne[Course](ctx, s.pool, courseSelect+` WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	course.Lessons, err = s.ListLessons(ctx, course.ID)
	return course, err
}

// GetCourseBySlug returns one course with its lessons.
func (s *Store) GetCourseBySlug(ctx context.Context, slug string, publishedOnly bool) (*Course, error) {
	q := courseSelect + ` WHERE slug = $1`
	if publishedOnly {
		q += ` AND status = 'published'`
	}
	course, err := queryOne[Course](ctx, s.pool, q, slug)
	if err != nil {
		return nil, err
	}
	course.Lessons, err = s.ListLessons(ctx, course.ID)
	return course, err
}

// CreateCourse inserts a course, generating a unique slug.
func (s *Store) CreateCourse(ctx context.Context, c *Course) error {
	base := c.Slug
	if base == "" {
		base = c.Title
	}
	slug, err := s.uniqueSlug(ctx, "courses", slugify(base), 0)
	if err != nil {
		return err
	}
	c.Slug = slug
	if c.Status == "" {
		c.Status = "published"
	}
	if c.Level == "" {
		c.Level = "Beginner"
	}
	if c.Language == "" {
		c.Language = "English"
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO courses (slug, title, summary, description, cover_image, level, duration, instructor,
		                     category, language, promo_video, prerequisites, outcomes,
		                     price_cents, status, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING id, created_at, updated_at`,
		c.Slug, c.Title, c.Summary, c.Description, c.CoverImage, c.Level,
		c.Duration, c.Instructor, c.Category, c.Language, c.PromoVideo,
		c.Prerequisites, c.Outcomes, c.PriceCents, c.Status, c.SortOrder,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

// UpdateCourse saves changes to an existing course.
func (s *Store) UpdateCourse(ctx context.Context, c *Course) error {
	base := c.Slug
	if base == "" {
		base = c.Title
	}
	slug, err := s.uniqueSlug(ctx, "courses", slugify(base), c.ID)
	if err != nil {
		return err
	}
	c.Slug = slug
	if c.Status == "" {
		c.Status = "published"
	}
	if c.Level == "" {
		c.Level = "Beginner"
	}
	if c.Language == "" {
		c.Language = "English"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE courses
		SET slug=$1, title=$2, summary=$3, description=$4, cover_image=$5, level=$6,
		    duration=$7, instructor=$8, category=$9, language=$10, promo_video=$11,
		    prerequisites=$12, outcomes=$13, price_cents=$14, status=$15, sort_order=$16,
		    updated_at=now()
		WHERE id=$17`,
		c.Slug, c.Title, c.Summary, c.Description, c.CoverImage, c.Level,
		c.Duration, c.Instructor, c.Category, c.Language, c.PromoVideo,
		c.Prerequisites, c.Outcomes, c.PriceCents, c.Status, c.SortOrder, c.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCourse removes a course and its lessons.
func (s *Store) DeleteCourse(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM courses WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Lessons ---

// ListLessons returns the lessons of a course, ordered for display.
func (s *Store) ListLessons(ctx context.Context, courseID int64) ([]Lesson, error) {
	return queryRows[Lesson](ctx, s.pool,
		lessonSelect+` WHERE course_id = $1 ORDER BY sort_order, id`, courseID)
}

// GetLessonByID returns one lesson by id.
func (s *Store) GetLessonByID(ctx context.Context, id int64) (*Lesson, error) {
	return queryOne[Lesson](ctx, s.pool, lessonSelect+` WHERE id = $1`, id)
}

// CreateLesson inserts a lesson under a course, generating a unique slug.
func (s *Store) CreateLesson(ctx context.Context, l *Lesson) error {
	base := l.Slug
	if base == "" {
		base = l.Title
	}
	slug, err := s.uniqueSlug(ctx, "lessons", slugify(base), 0)
	if err != nil {
		return err
	}
	l.Slug = slug
	return s.pool.QueryRow(ctx, `
		INSERT INTO lessons (course_id, module, slug, title, content, video_url, duration, is_preview, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at`,
		l.CourseID, l.Module, l.Slug, l.Title, l.Content, l.VideoURL, l.Duration, l.IsPreview, l.SortOrder,
	).Scan(&l.ID, &l.CreatedAt, &l.UpdatedAt)
}

// UpdateLesson saves changes to an existing lesson.
func (s *Store) UpdateLesson(ctx context.Context, l *Lesson) error {
	base := l.Slug
	if base == "" {
		base = l.Title
	}
	slug, err := s.uniqueSlug(ctx, "lessons", slugify(base), l.ID)
	if err != nil {
		return err
	}
	l.Slug = slug
	tag, err := s.pool.Exec(ctx, `
		UPDATE lessons
		SET module=$1, slug=$2, title=$3, content=$4, video_url=$5, duration=$6,
		    is_preview=$7, sort_order=$8, updated_at=now()
		WHERE id=$9`,
		l.Module, l.Slug, l.Title, l.Content, l.VideoURL, l.Duration, l.IsPreview, l.SortOrder, l.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderLessons updates module + sort_order for each lesson in a single
// transaction. Used by the course wizard when a user drags lessons into a
// new arrangement (possibly across modules).
func (s *Store) ReorderLessons(ctx context.Context, courseID int64, items []Lesson) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, l := range items {
		if _, err := tx.Exec(ctx, `
			UPDATE lessons SET module = $1, sort_order = $2, updated_at = now()
			WHERE id = $3 AND course_id = $4`,
			l.Module, l.SortOrder, l.ID, courseID,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteLesson removes a lesson by id.
func (s *Store) DeleteLesson(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM lessons WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
