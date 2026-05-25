-- Extend newsletter_subscribers with the columns we need to run a real
-- mailing list: tags for audience targeting, source (where they signed up
-- from), an optional link to a user account, and an unsubscribe token so
-- public unsubscribe links are tamper-proof.
ALTER TABLE newsletter_subscribers
    ADD COLUMN IF NOT EXISTS name              TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS tags              TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    ADD COLUMN IF NOT EXISTS source            TEXT NOT NULL DEFAULT 'website',
    ADD COLUMN IF NOT EXISTS user_id           BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS unsubscribe_token TEXT,
    ADD COLUMN IF NOT EXISTS unsubscribed_at   TIMESTAMPTZ;

-- Generate a token for any pre-existing rows so old subscribers can also
-- use the unsubscribe link in newsletters.
UPDATE newsletter_subscribers
SET unsubscribe_token = md5(random()::text || id::text || extract(epoch from now())::text)
WHERE unsubscribe_token IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_newsletter_subscribers_token
    ON newsletter_subscribers(unsubscribe_token)
    WHERE unsubscribe_token IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_newsletter_subscribers_tags
    ON newsletter_subscribers USING GIN (tags);

CREATE INDEX IF NOT EXISTS idx_newsletter_subscribers_user
    ON newsletter_subscribers(user_id);

-- Newsletters: an admin-composed email blast targeting a tag-based
-- audience. We snapshot sent_count + sent_at at send time so admins can
-- see history even if subscribers later leave.
CREATE TABLE IF NOT EXISTS newsletters (
    id            BIGSERIAL PRIMARY KEY,
    subject       TEXT NOT NULL,
    body          TEXT NOT NULL DEFAULT '',     -- rich-text/html body
    audience_tags TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    audience_all  BOOLEAN NOT NULL DEFAULT FALSE,
    status        TEXT NOT NULL DEFAULT 'draft', -- 'draft' | 'sent'
    sent_count    INTEGER NOT NULL DEFAULT 0,
    sent_at       TIMESTAMPTZ,
    created_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_newsletters_status ON newsletters(status);
