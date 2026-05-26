-- 0016_seed_education_service.sql
-- Seed an Education service entry. Idempotent: ON CONFLICT lets the
-- migration be safely re-run, and admins can edit the title / copy /
-- icon / sort order from /admin/services afterward without the seed
-- ever overwriting their changes.

INSERT INTO services (slug, title, summary, body, icon, sort_order, status)
VALUES (
    'education',
    'Education',
    'Programmes, workshops, and courseware that help individuals and teams grow into the next generation of African creatives and technologists.',
    '<p>Our Education arm runs guided courses, cohort programmes, and bespoke workshops for organisations. Whether you''re a learner starting out, an existing team upskilling, or an institution building a curriculum, we partner with you to ship learning that actually sticks.</p><p>Reach out through the contact form or explore our open courses from the Courses page.</p>',
    '🎓',
    100,
    'published'
)
ON CONFLICT (slug) DO NOTHING;
