package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// CourseTask is an assignment shown at the end of a module.
type CourseTask struct {
	ID           int64     `json:"id" db:"id"`
	CourseID     int64     `json:"courseId" db:"course_id"`
	Module       string    `json:"module" db:"module"`
	Prompt       string    `json:"prompt" db:"prompt"`
	RequiredPass bool      `json:"requiredPass" db:"required_pass"`
	SortOrder    int       `json:"sortOrder" db:"sort_order"`
	CreatedAt    time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time `json:"updatedAt" db:"updated_at"`
}

// CourseTaskSubmission is the student's response to a task. Grade is
// blank while pending, then 'passed' / 'failed' once admin moderates.
type CourseTaskSubmission struct {
	ID          int64      `json:"id" db:"id"`
	TaskID      int64      `json:"taskId" db:"task_id"`
	UserID      int64      `json:"userId" db:"user_id"`
	Body        string     `json:"body" db:"body"`
	FileURL     string     `json:"fileUrl" db:"file_url"`
	Grade       string     `json:"grade" db:"grade"`
	Feedback    string     `json:"feedback" db:"feedback"`
	SubmittedAt time.Time  `json:"submittedAt" db:"submitted_at"`
	GradedAt    *time.Time `json:"gradedAt" db:"graded_at"`
	GraderID    *int64     `json:"graderId" db:"grader_id"`
}

// AdminTaskSubmission joins the user + task + course context that the
// admin needs to grade a submission without click-throughs.
type AdminTaskSubmission struct {
	CourseTaskSubmission
	StudentName  string `json:"studentName"`
	StudentEmail string `json:"studentEmail"`
	CourseTitle  string `json:"courseTitle"`
	CourseID     int64  `json:"courseId"`
	TaskPrompt   string `json:"taskPrompt"`
	TaskModule   string `json:"taskModule"`
}

const courseTaskSelect = `SELECT id, course_id, module, prompt, required_pass, sort_order, created_at, updated_at FROM course_tasks`

// ListCourseTasks returns all tasks attached to a course, ordered for
// the lesson runner (per-module, then sort_order).
func (s *Store) ListCourseTasks(ctx context.Context, courseID int64) ([]CourseTask, error) {
	return queryRows[CourseTask](ctx, s.pool,
		courseTaskSelect+` WHERE course_id = $1 ORDER BY module, sort_order, id`,
		courseID)
}

// AddCourseTask creates a new task pinned to a module name.
func (s *Store) AddCourseTask(ctx context.Context, t *CourseTask) error {
	var nextPos int
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM course_tasks WHERE course_id = $1 AND module = $2`,
		t.CourseID, t.Module).Scan(&nextPos); err != nil {
		return err
	}
	t.SortOrder = nextPos
	return s.pool.QueryRow(ctx, `
		INSERT INTO course_tasks (course_id, module, prompt, required_pass, sort_order)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`,
		t.CourseID, t.Module, t.Prompt, t.RequiredPass, t.SortOrder,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

func (s *Store) UpdateCourseTask(ctx context.Context, t *CourseTask) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE course_tasks
		   SET module = $1, prompt = $2, required_pass = $3, updated_at = now()
		 WHERE id = $4 AND course_id = $5`,
		t.Module, t.Prompt, t.RequiredPass, t.ID, t.CourseID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteCourseTask(ctx context.Context, courseID, taskID int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM course_tasks WHERE id = $1 AND course_id = $2`,
		taskID, courseID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Submissions ---

// UpsertSubmission inserts a new submission or replaces the user's
// previous attempt for the same task. Re-submitting clears the grade
// so an admin can re-moderate.
func (s *Store) UpsertSubmission(ctx context.Context, sub *CourseTaskSubmission) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO course_task_submissions (task_id, user_id, body, file_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (task_id, user_id) DO UPDATE
		    SET body         = EXCLUDED.body,
		        file_url     = EXCLUDED.file_url,
		        grade        = '',
		        feedback     = '',
		        submitted_at = now(),
		        graded_at    = NULL,
		        grader_id    = NULL
		RETURNING id, grade, feedback, submitted_at, graded_at, grader_id`,
		sub.TaskID, sub.UserID, sub.Body, sub.FileURL,
	).Scan(&sub.ID, &sub.Grade, &sub.Feedback, &sub.SubmittedAt, &sub.GradedAt, &sub.GraderID)
}

// GetUserSubmissionsForCourse returns the caller's submissions for
// every task on this course, keyed by task id by the API layer.
func (s *Store) GetUserSubmissionsForCourse(ctx context.Context, userID, courseID int64) ([]CourseTaskSubmission, error) {
	return queryRows[CourseTaskSubmission](ctx, s.pool, `
		SELECT s.id, s.task_id, s.user_id, s.body, s.file_url, s.grade, s.feedback,
		       s.submitted_at, s.graded_at, s.grader_id
		FROM course_task_submissions s
		JOIN course_tasks t ON t.id = s.task_id
		WHERE t.course_id = $1 AND s.user_id = $2
		ORDER BY s.submitted_at DESC`,
		courseID, userID)
}

// AdminListSubmissionsForCourse returns every submission for the
// course with student + task context joined in.
func (s *Store) AdminListSubmissionsForCourse(ctx context.Context, courseID int64) ([]AdminTaskSubmission, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.task_id, s.user_id, s.body, s.file_url, s.grade, s.feedback,
		       s.submitted_at, s.graded_at, s.grader_id,
		       u.name, u.email,
		       c.title, c.id,
		       t.prompt, t.module
		FROM course_task_submissions s
		JOIN course_tasks t ON t.id = s.task_id
		JOIN courses      c ON c.id = t.course_id
		JOIN users        u ON u.id = s.user_id
		WHERE t.course_id = $1
		ORDER BY s.submitted_at DESC, s.id DESC`,
		courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminTaskSubmission{}
	for rows.Next() {
		var a AdminTaskSubmission
		if err := rows.Scan(&a.ID, &a.TaskID, &a.UserID, &a.Body, &a.FileURL, &a.Grade, &a.Feedback,
			&a.SubmittedAt, &a.GradedAt, &a.GraderID,
			&a.StudentName, &a.StudentEmail,
			&a.CourseTitle, &a.CourseID,
			&a.TaskPrompt, &a.TaskModule); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CourseIDForTask returns the course id that owns the given task, or
// ErrNotFound. Used by submitCourseTask to enforce enrollment.
func (s *Store) CourseIDForTask(ctx context.Context, taskID int64) (int64, error) {
	var courseID int64
	err := s.pool.QueryRow(ctx,
		`SELECT course_id FROM course_tasks WHERE id = $1`, taskID).Scan(&courseID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return courseID, nil
}

// SubmissionContext returns (user_id, course_id) for a submission —
// used by callers that need to trigger course-level side effects
// (cert auto-issue) without re-fetching the full submission row.
func (s *Store) SubmissionContext(ctx context.Context, submissionID int64) (userID, courseID int64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT s.user_id, t.course_id
		FROM course_task_submissions s
		JOIN course_tasks t ON t.id = s.task_id
		WHERE s.id = $1`,
		submissionID).Scan(&userID, &courseID)
	return userID, courseID, err
}

// GradeSubmission sets the grade + feedback and stamps graded_at.
func (s *Store) GradeSubmission(ctx context.Context, submissionID, graderID int64, grade, feedback string) error {
	if grade != "passed" && grade != "failed" {
		return ErrNotFound // caller validates first
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE course_task_submissions
		   SET grade = $1, feedback = $2, graded_at = now(), grader_id = $3
		 WHERE id = $4`,
		grade, feedback, graderID, submissionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
