-- Profile/address fields on users so customers can save shipping +
-- billing info once and have it pre-fill checkout, and the same record
-- is used to render their name + contact info on course certificates.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS phone          TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS address_line1  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS address_line2  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS city           TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS state          TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS country        TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS postal_code    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS avatar         TEXT NOT NULL DEFAULT '';

-- Support tickets (labelled "Complaints" in the customer UI). category
-- lets the team route to the right person; status moves open → replied
-- (admin responded, waiting on customer) → closed.
CREATE TABLE IF NOT EXISTS tickets (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subject    TEXT NOT NULL,
    category   TEXT NOT NULL DEFAULT 'general',
    status     TEXT NOT NULL DEFAULT 'open',
    last_reply_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tickets_user ON tickets(user_id);
CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status);

-- Each ticket reply (or the original body) is one row here. author_role
-- distinguishes the customer's messages from staff replies in the thread
-- view without needing to look up the user every time.
CREATE TABLE IF NOT EXISTS ticket_messages (
    id          BIGSERIAL PRIMARY KEY,
    ticket_id   BIGINT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    author_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    author_role TEXT NOT NULL,
    author_name TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ticket_messages_ticket ON ticket_messages(ticket_id);

-- Testimonials gain a link to the submitter and a `source` so admin can
-- spot which ones came in via the customer dashboard vs. were typed in
-- by staff. Customer-submitted rows land with status='pending' and only
-- show on the public site once an admin flips them to 'published'.
ALTER TABLE testimonials
    ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS source  TEXT NOT NULL DEFAULT 'admin',
    ADD COLUMN IF NOT EXISTS submitted_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_testimonials_user ON testimonials(user_id);
CREATE INDEX IF NOT EXISTS idx_testimonials_status ON testimonials(status);
