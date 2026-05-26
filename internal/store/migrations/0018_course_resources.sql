-- 0018_course_resources.sql
-- A single resources table covers two scopes:
--   - lesson_id IS NOT NULL  → attached to that lesson, shown in the lesson runner
--   - lesson_id IS NULL      → course-wide, shown on the course landing page
--
-- Resources can be a URL (label + url) OR an uploaded file (label + url
-- where url is the path returned by /api/admin/upload-file). The admin
-- form picks whichever; the public side just renders whatever URL is set.

CREATE TABLE IF NOT EXISTS course_resources (
    id         BIGSERIAL PRIMARY KEY,
    course_id  BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    lesson_id  BIGINT REFERENCES lessons(id) ON DELETE CASCADE,
    label      TEXT NOT NULL,
    url        TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'link', -- 'link' | 'file' (informational only)
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_course_resources_course
    ON course_resources(course_id, sort_order);
CREATE INDEX IF NOT EXISTS idx_course_resources_lesson
    ON course_resources(lesson_id, sort_order)
    WHERE lesson_id IS NOT NULL;
