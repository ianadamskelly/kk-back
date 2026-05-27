-- 0023_service_taxonomy_and_copy.sql
-- Align the agency offer to three public pillars and provide structured
-- capability copy for each parent service detail page.

ALTER TABLE services
    ADD COLUMN IF NOT EXISTS pillar TEXT NOT NULL DEFAULT '';

ALTER TABLE services
    DROP CONSTRAINT IF EXISTS services_pillar_check;
ALTER TABLE services
    ADD CONSTRAINT services_pillar_check
    CHECK (pillar IN ('', 'brand_identity', 'digital_platforms', 'content_growth'));

CREATE TABLE IF NOT EXISTS service_subservices (
    id          BIGSERIAL PRIMARY KEY,
    service_id  BIGINT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    sort_order  INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'published'
                    CHECK (status IN ('draft', 'published')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (service_id, title)
);

CREATE INDEX IF NOT EXISTS idx_service_subservices_public
    ON service_subservices(service_id, status, sort_order);

-- These are the approved client-facing parent offerings. Upserting also
-- repairs fresh databases where the previous Education migration caused the
-- normal seed routine to see a non-empty services table and stop early.
INSERT INTO services (slug, title, summary, body, icon, pillar, sort_order, status)
VALUES
    ('branding', 'Brand Identity',
     'Clear identity systems that help growing brands look consistent, credible, and memorable.',
     '<p>We shape the identity your audience recognises: strategy, logos, visual systems, and practical guidance your team can use consistently.</p>',
     '✦', 'brand_identity', 1, 'published'),
    ('graphic-design', 'Graphic Design',
     'Purposeful visual assets for campaigns, products, presentations, and everyday brand communication.',
     '<p>We translate your message into polished visual work across print and digital touchpoints, keeping every piece recognisably on brand.</p>',
     '✎', 'brand_identity', 2, 'published'),
    ('branded-merchandise', 'Branded Merchandise',
     'Wearable and tangible brand designs that extend your identity beyond the screen.',
     '<p>We design merchandise concepts and artwork that feel like a natural expression of your brand, ready for your chosen production partner.</p>',
     '✧', 'brand_identity', 3, 'published'),
    ('web-development', 'Web Development',
     'Fast, accessible websites and commerce experiences built around your business goals.',
     '<p>We design and build websites that make your offer easy to understand, easy to use, and ready to support growth.</p>',
     '⌘', 'digital_platforms', 4, 'published'),
    ('animation-video', 'Animation & Video',
     'Motion-led storytelling that explains ideas and gives your content more energy.',
     '<p>From explainers to social motion, we develop animated and video content that communicates clearly and earns attention.</p>',
     '▶', 'content_growth', 5, 'published'),
    ('photography-videography', 'Photography & Videography',
     'Original photo and video content that captures your people, products, and moments.',
     '<p>We plan and create brand-aligned visuals for social channels, launches, products, events, and campaigns.</p>',
     '◉', 'content_growth', 6, 'published'),
    ('online-presence-management', 'Content & Online Presence',
     'Consistent content systems that keep your brand useful, visible, and active online.',
     '<p>We help you plan and manage valuable content, including editorial publishing and client-owned learning experiences.</p>',
     '❖', 'content_growth', 7, 'published'),
    ('digital-marketing', 'Digital Marketing',
     'Organic digital strategy focused on reaching the right audience and improving discoverability.',
     '<p>We support your growth through social strategy and search optimisation built around clear goals and useful content.</p>',
     '↗', 'content_growth', 8, 'published')
ON CONFLICT (slug) DO UPDATE SET
    title = EXCLUDED.title,
    summary = EXCLUDED.summary,
    body = EXCLUDED.body,
    icon = EXCLUDED.icon,
    pillar = EXCLUDED.pillar,
    sort_order = EXCLUDED.sort_order,
    status = EXCLUDED.status,
    updated_at = now();

UPDATE services
SET status = 'draft', pillar = '', updated_at = now()
WHERE slug = 'education';

-- Keep the public catalogue to the reviewed offer inventory. Existing records
-- remain available to admins for reference but must be intentionally republished.
UPDATE services
SET status = 'draft', pillar = '', updated_at = now()
WHERE slug NOT IN (
    'branding',
    'graphic-design',
    'branded-merchandise',
    'web-development',
    'animation-video',
    'photography-videography',
    'online-presence-management',
    'digital-marketing',
    'education'
);

WITH approved(slug, title, summary, body, sort_order) AS (
    VALUES
    ('branding', 'Logo Design',
     'A distinctive mark built to work across the places your brand appears.',
     '<p>We create logo directions rooted in your audience and positioning, then refine the chosen direction into practical variations for daily use.</p>', 1),
    ('branding', 'Visual Identity Systems',
     'Colour, type, imagery, and layout working together as one recognisable system.',
     '<p>We build the supporting visual language around your mark so your brand remains coherent across digital and printed communication.</p>', 2),
    ('branding', 'Brand Strategy',
     'Clarity on your audience, value, voice, and creative direction before design begins.',
     '<p>We turn business goals and audience insight into a focused foundation that informs the identity and future communication.</p>', 3),
    ('branding', 'Brand Guidelines',
     'Straightforward rules and templates that help your team use the brand consistently.',
     '<p>We document logo, colour, typography, imagery, and tone decisions so strong work stays consistent as you grow.</p>', 4),

    ('branded-merchandise', 'Apparel Design',
     'Branded apparel concepts designed to feel wearable and unmistakably yours.',
     '<p>We create artwork and placements for apparel that express your identity clearly and translate well into production.</p>', 1),
    ('branded-merchandise', 'Custom Merchandise Design',
     'Thoughtful branded items for teams, launches, events, and communities.',
     '<p>We extend your visual identity into selected merchandise concepts and supply artwork prepared for a production partner.</p>', 2),

    ('graphic-design', 'Marketing Collateral',
     'Campaign and sales materials that communicate your offer with clarity.',
     '<p>We design brochures, flyers, pitch materials, and supporting assets that organise information and make your brand look considered.</p>', 1),
    ('graphic-design', 'Digital Content Design',
     'Branded visual assets for social channels, web banners, and digital communication.',
     '<p>We create flexible content systems and campaign visuals that remain consistent across sizes, channels, and moments.</p>', 2),
    ('graphic-design', 'Packaging Design',
     'Packaging visuals that strengthen recognition and present your product confidently.',
     '<p>We translate your identity onto packaging surfaces, balancing impact, information, and practical production requirements.</p>', 3),
    ('graphic-design', 'Custom Illustration',
     'Original illustrated elements that give your message a distinct character.',
     '<p>We develop brand-aligned illustrations for editorial content, interfaces, campaigns, and packaging where stock visuals will not do.</p>', 4),

    ('animation-video', 'Explainer Videos',
     'Short visual stories that make a product, service, or idea easier to understand.',
     '<p>We plan the narrative, visual direction, and motion needed to communicate complex ideas simply and memorably.</p>', 1),
    ('animation-video', 'Educational Animation',
     'Motion content designed to support understanding and engagement.',
     '<p>We transform learning material into focused visual sequences that clarify concepts without overwhelming the viewer.</p>', 2),
    ('animation-video', 'Social Media Animation',
     'Brief, platform-ready motion pieces made to earn attention in the feed.',
     '<p>We create branded motion content for launches, announcements, and recurring social storytelling.</p>', 3),
    ('animation-video', '2D/3D Animation',
     'Crafted motion and dimensional visuals for stories that need greater depth.',
     '<p>We develop animation styles suited to your message, from graphic 2D movement to selected 3D visualisation.</p>', 4),

    ('photography-videography', 'Social Media Photo & Video',
     'Original visual content planned for consistent social storytelling.',
     '<p>We capture photo and video assets designed for the formats, pace, and tone of your active channels.</p>', 1),
    ('photography-videography', 'Event Photography & Videography',
     'Coverage that records the atmosphere, people, and purpose of your event.',
     '<p>We document important moments with usable visual stories for recap, promotion, and ongoing communication.</p>', 2),
    ('photography-videography', 'Product Photography',
     'Clean product imagery that helps customers understand quality and detail.',
     '<p>We art-direct and capture product visuals aligned to your identity and suitable for web, social, and campaign use.</p>', 3),
    ('photography-videography', 'Commercial Videography',
     'Brand video content built around a clear audience and communication goal.',
     '<p>We guide concept, planning, filming, and editing to produce purposeful video for your business or campaign.</p>', 4),

    ('web-development', 'Website Design & Development',
     'A custom website experience shaped around your brand and customer journey.',
     '<p>We design and build responsive, accessible sites that make your message clear and your next action easy to take.</p>', 1),
    ('web-development', 'E-commerce Development',
     'Online shopping experiences designed for trust, clarity, and smooth checkout.',
     '<p>We build commerce interfaces that present products well and help customers move confidently from discovery to purchase.</p>', 2),
    ('web-development', 'Website Maintenance & Support',
     'Practical ongoing support that keeps your digital platform dependable.',
     '<p>We support content updates, improvements, and technical upkeep so the site continues to serve your team and visitors.</p>', 3),

    ('online-presence-management', 'Blog & Content Management',
     'Useful published content that develops authority and keeps your site active.',
     '<p>We help plan, create, and manage content that supports audience questions, brand voice, and organic discoverability.</p>', 1),
    ('online-presence-management', 'Online Course Development',
     'Client-owned learning experiences structured for clear digital delivery.',
     '<p>We help organisations turn expertise into well-structured course content and an engaging online learning experience; this is distinct from Kuza Kizazi''s own courses.</p>', 2),

    ('digital-marketing', 'Social Media Marketing',
     'Organic social direction that connects consistent content to business goals.',
     '<p>We shape channel strategy, content themes, and publishing rhythms that make your brand easier to follow and trust.</p>', 1),
    ('digital-marketing', 'Search Engine Optimisation',
     'Search foundations that help the right people discover your website.',
     '<p>We improve content and on-page search signals around real audience needs, clearer structure, and sustainable visibility.</p>', 2)
)
INSERT INTO service_subservices (service_id, title, summary, body, sort_order, status)
SELECT s.id, a.title, a.summary, a.body, a.sort_order, 'published'
FROM approved a
JOIN services s ON s.slug = a.slug
ON CONFLICT (service_id, title) DO UPDATE SET
    summary = EXCLUDED.summary,
    body = EXCLUDED.body,
    sort_order = EXCLUDED.sort_order,
    status = EXCLUDED.status,
    updated_at = now();

UPDATE site_settings
SET value = 'Kuza Kizazi'
WHERE key = 'site_name' AND value = 'Kuza Kizazi Kreative';

UPDATE site_settings
SET value = 'Creative agency · Nairobi'
WHERE key = 'tagline' AND value IN (
    'Unleashing Creativity, Empowering Possibilities',
    'Unleashing creativity, empowering possibilities.'
);

UPDATE site_settings
SET value = 'We turn bold visions into digital reality.'
WHERE key = 'hero_title' AND value IN (
    'TOP GRAPHIC DESIGN AGENCY IN KENYA',
    'Unleashing Creativity, Empowering Possibilities',
    'Elevate Your Business with Our Comprehensive Consulting Services',
    'Revolutionize Your Business Strategy with Our Expertise',
    'Introducing Our New Business Consulting Solutions!'
);

UPDATE site_settings
SET value = 'Kuza Kizazi is a Nairobi creative agency crafting brands, websites, and stories that move people.'
WHERE key = 'hero_subtitle' AND (
    value ILIKE '%Kuza Kizazi Kreative%'
    OR value ILIKE '%Comprehensive Consulting Services%'
    OR value ILIKE '%Business Consulting Solutions%'
);

UPDATE site_settings
SET value = 'Helping growing African brands build their identity, digital presence, and content systems through strategy, design, websites, media, and marketing.'
WHERE key = 'footer_description'
  AND value = 'A dynamic and forward-thinking global company dedicated to empowering the next generation through a diverse range of creative and innovative services.';

-- Remove known template content from publication rather than rewriting
-- authored CMS material without editorial approval.
UPDATE posts
SET status = 'draft', updated_at = now()
WHERE status = 'published'
  AND (
    title || ' ' || excerpt || ' ' || content
  ) ILIKE ANY (ARRAY[
    '%Elevate Your Business with Our Comprehensive Consulting Services%',
    '%Revolutionize Your Business Strategy with Our Expertise%',
    '%Introducing Our New Business Consulting Solutions!%'
  ]);

UPDATE projects
SET status = 'draft', updated_at = now()
WHERE status = 'published'
  AND (
    slug = 'innovation-hub-navigating-the-future'
    OR (title || ' ' || summary || ' ' || body) ILIKE ANY (ARRAY[
        '%Elevate Your Business with Our Comprehensive Consulting Services%',
        '%Revolutionize Your Business Strategy with Our Expertise%',
        '%Introducing Our New Business Consulting Solutions!%',
        '%Our Corporate Business Planning%'
    ])
  );

UPDATE products
SET status = 'draft', updated_at = now()
WHERE status = 'published'
  AND (
    name || ' ' || description || ' ' || body
  ) ILIKE ANY (ARRAY[
    '%Elevate Your Business with Our Comprehensive Consulting Services%',
    '%Revolutionize Your Business Strategy with Our Expertise%',
    '%Introducing Our New Business Consulting Solutions!%'
  ]);

UPDATE testimonials
SET quote = 'I got a lot of insight and material from the content that is posted.'
WHERE quote = 'I got a lot if insight and material from the content that is posted.';
