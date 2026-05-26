-- 0017_reviews.sql
-- A single reviews table for both products and courses (and future
-- entity types if we ever want to review services or library items).
-- Verified-buyer enforcement happens in the API layer, not here.

CREATE TABLE IF NOT EXISTS reviews (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type TEXT   NOT NULL,
    entity_id   BIGINT NOT NULL,
    rating      INTEGER NOT NULL,
    body        TEXT   NOT NULL DEFAULT '',
    status      TEXT   NOT NULL DEFAULT 'pending',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE reviews
    DROP CONSTRAINT IF EXISTS reviews_entity_type_check;
ALTER TABLE reviews
    ADD  CONSTRAINT reviews_entity_type_check CHECK (entity_type IN ('product', 'course'));

ALTER TABLE reviews
    DROP CONSTRAINT IF EXISTS reviews_status_check;
ALTER TABLE reviews
    ADD  CONSTRAINT reviews_status_check CHECK (status IN ('pending', 'published', 'rejected'));

ALTER TABLE reviews
    DROP CONSTRAINT IF EXISTS reviews_rating_check;
ALTER TABLE reviews
    ADD  CONSTRAINT reviews_rating_check CHECK (rating BETWEEN 1 AND 5);

-- One review per (user, entity). Editing replaces; deleting frees the slot.
CREATE UNIQUE INDEX IF NOT EXISTS idx_reviews_unique_user_entity
    ON reviews(user_id, entity_type, entity_id);

CREATE INDEX IF NOT EXISTS idx_reviews_entity
    ON reviews(entity_type, entity_id, status);

CREATE INDEX IF NOT EXISTS idx_reviews_status
    ON reviews(status, created_at DESC);
