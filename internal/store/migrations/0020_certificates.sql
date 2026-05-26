-- 0020_certificates.sql
-- One row per certificate issued. `code` is the public-facing
-- identifier surfaced in URLs and on the printed PDF — opaque enough
-- that nobody can guess one off-the-cuff but short enough to be
-- legible. UNIQUE(user_id, course_id) ensures a single certificate
-- per user per course.

CREATE TABLE IF NOT EXISTS certificates (
    id         BIGSERIAL PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    course_id  BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    issued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, course_id)
);

CREATE INDEX IF NOT EXISTS idx_certificates_user
    ON certificates(user_id, issued_at DESC);
