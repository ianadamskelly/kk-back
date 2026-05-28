ALTER TABLE memberships
    ADD COLUMN IF NOT EXISTS plan TEXT NOT NULL DEFAULT 'full';

ALTER TABLE memberships
    DROP CONSTRAINT IF EXISTS memberships_plan_check;

ALTER TABLE memberships
    ADD CONSTRAINT memberships_plan_check
    CHECK (plan IN ('full', 'library'));

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS membership_plan TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_memberships_plan ON memberships(plan);
