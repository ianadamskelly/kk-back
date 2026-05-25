-- LMS-flavoured fields on courses (marketing + meta) and a free-preview
-- flag on lessons so sample lessons can be shown to non-buyers.
ALTER TABLE courses
    ADD COLUMN IF NOT EXISTS category      TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS language      TEXT NOT NULL DEFAULT 'English',
    ADD COLUMN IF NOT EXISTS promo_video   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prerequisites TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS outcomes      TEXT NOT NULL DEFAULT '';

ALTER TABLE lessons
    ADD COLUMN IF NOT EXISTS is_preview BOOLEAN NOT NULL DEFAULT FALSE;
