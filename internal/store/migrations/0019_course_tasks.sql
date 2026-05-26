-- 0019_course_tasks.sql
-- Per-module tasks (assignments) + student submissions.
--
-- A course_task lives at the end of a module — `module` is just the
-- module name string the lessons already share. The lesson runner
-- shows the task on the last lesson of that module.
--
-- A course_task_submission is the student's response — text body and
-- optional uploaded file. Grade is admin-set: 'passed' | 'failed' (or
-- empty while pending). required_pass on the parent task indicates
-- whether the rest of the course is gated on a passing grade
-- (enforcement is currently informational on the customer side).

CREATE TABLE IF NOT EXISTS course_tasks (
    id            BIGSERIAL PRIMARY KEY,
    course_id     BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    module        TEXT NOT NULL,
    prompt        TEXT NOT NULL,
    required_pass BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order    INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_course_tasks_course_module
    ON course_tasks(course_id, module, sort_order);

CREATE TABLE IF NOT EXISTS course_task_submissions (
    id           BIGSERIAL PRIMARY KEY,
    task_id      BIGINT NOT NULL REFERENCES course_tasks(id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body         TEXT NOT NULL DEFAULT '',
    file_url     TEXT NOT NULL DEFAULT '',
    grade        TEXT NOT NULL DEFAULT '', -- '' | 'passed' | 'failed'
    feedback     TEXT NOT NULL DEFAULT '',
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    graded_at    TIMESTAMPTZ,
    grader_id    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    UNIQUE (task_id, user_id)
);

ALTER TABLE course_task_submissions
    DROP CONSTRAINT IF EXISTS course_task_submissions_grade_check;
ALTER TABLE course_task_submissions
    ADD CONSTRAINT course_task_submissions_grade_check
        CHECK (grade IN ('', 'passed', 'failed'));

CREATE INDEX IF NOT EXISTS idx_course_task_submissions_task
    ON course_task_submissions(task_id, submitted_at DESC);
CREATE INDEX IF NOT EXISTS idx_course_task_submissions_user
    ON course_task_submissions(user_id, submitted_at DESC);
