package store

import (
	"context"

	"golang.org/x/crypto/bcrypt"
)

// Seed populates an empty database with an admin account and sample content.
// Every section is guarded by a count check, so it is safe to run on every boot.
func (s *Store) Seed(ctx context.Context, adminEmail, adminPassword string) error {
	if err := s.seedRoles(ctx); err != nil {
		return err
	}
	if err := s.seedAdmin(ctx, adminEmail, adminPassword); err != nil {
		return err
	}
	if err := s.seedSettings(ctx); err != nil {
		return err
	}
	if err := s.seedStats(ctx); err != nil {
		return err
	}
	if err := s.seedServices(ctx); err != nil {
		return err
	}
	if err := s.seedProjects(ctx); err != nil {
		return err
	}
	if err := s.seedTeam(ctx); err != nil {
		return err
	}
	if err := s.seedTestimonials(ctx); err != nil {
		return err
	}
	if err := s.seedBlog(ctx); err != nil {
		return err
	}
	if err := s.seedShop(ctx); err != nil {
		return err
	}
	if err := s.seedLearn(ctx); err != nil {
		return err
	}
	return s.seedLibrary(ctx)
}

func (s *Store) seedLearn(ctx context.Context) error {
	existing, err := s.ListCourses(ctx, false)
	if err != nil || len(existing) > 0 {
		return err
	}

	type lessonSeed struct {
		module, title, content, duration string
	}
	type courseSeed struct {
		course  Course
		lessons []lessonSeed
	}
	courses := []courseSeed{
		{
			course: Course{
				Title:       "Brand Identity Fundamentals",
				Summary:     "Learn how to build a brand that is clear, consistent, and unmistakably yours.",
				Description: "A practical, project-based introduction to brand identity — from research and strategy through to a logo system and the guidelines that hold it all together.",
				Level:       "Beginner", Duration: "3 hours", Instructor: "Sarah Chen",
				SortOrder: 1,
			},
			lessons: []lessonSeed{
				{"Getting started", "What is a brand, really?", "A brand is far more than a logo. In this lesson we define what a brand actually is — the promise, the personality, and the perception — and why that matters before any design begins.", "8 min"},
				{"Getting started", "Researching your market", "Good identity work starts with research. We look at how to study competitors, understand your audience, and find the white space your brand can own.", "12 min"},
				{"Building the identity", "Designing a logo system", "A modern logo is a system, not a single mark. Learn to design primary, secondary, and responsive variations that work everywhere.", "15 min"},
				{"Building the identity", "Choosing colour & type", "Colour and typography carry most of a brand's feeling. We cover building an accessible palette and a type scale that stays consistent.", "11 min"},
				{"Launch", "Brand guidelines that stick", "Guidelines only work if people use them. We finish by assembling a concise, practical brand guide your whole team can follow.", "10 min"},
			},
		},
		{
			course: Course{
				Title:       "Web Design with Intent",
				Summary:     "Design websites that do a job — not just look good.",
				Description: "An intermediate course on designing purposeful, high-performing websites: outcomes, layout, responsive foundations, and a clean developer hand-off.",
				Level:       "Intermediate", Duration: "2.5 hours", Instructor: "Michael Okafor",
				SortOrder: 2,
			},
			lessons: []lessonSeed{
				{"Foundations", "Designing for outcomes", "Every page has a job. We start by defining the outcome of a page before touching layout, so design decisions have something to serve.", "9 min"},
				{"Foundations", "Layout & visual hierarchy", "How to guide the eye: grids, spacing, and contrast that make the most important thing feel the most important.", "13 min"},
				{"Building", "Responsive foundations", "Designing once for every screen. We cover a mobile-first mindset and the breakpoints that actually matter.", "12 min"},
				{"Building", "Handing off to developers", "A great design survives the build. Learn what developers need from you and how to document it clearly.", "10 min"},
			},
		},
	}

	for ci := range courses {
		cs := &courses[ci]
		cs.course.Status = "published"
		if err := s.CreateCourse(ctx, &cs.course); err != nil {
			return err
		}
		for li, ls := range cs.lessons {
			lesson := &Lesson{
				CourseID: cs.course.ID, Module: ls.module, Title: ls.title,
				Content: ls.content, Duration: ls.duration, SortOrder: li + 1,
			}
			if err := s.CreateLesson(ctx, lesson); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) seedLibrary(ctx context.Context) error {
	existing, err := s.ListLibrary(ctx, false)
	if err != nil || len(existing) > 0 {
		return err
	}
	resources := []LibraryResource{
		{Title: "2026 Brand Strategy Checklist", Type: "Guide", Category: "Branding", URL: "#", SortOrder: 1,
			Description: "A step-by-step checklist for defining your brand's foundations this year."},
		{Title: "Logo Presentation Template", Type: "Template", Category: "Design", URL: "#", SortOrder: 2,
			Description: "A polished template for presenting logo concepts to clients."},
		{Title: "Social Content Calendar", Type: "Template", Category: "Marketing", URL: "#", SortOrder: 3,
			Description: "Plan a month of on-brand social posts in one editable sheet."},
		{Title: "The Small Business Web Guide", Type: "E-book", Category: "Web", URL: "#", SortOrder: 4,
			Description: "Everything a small business needs to know before commissioning a website."},
		{Title: "Colour Palette Worksheet", Type: "Tool", Category: "Design", URL: "#", SortOrder: 5,
			Description: "A worksheet for building an accessible, balanced brand palette."},
		{Title: "Storyboarding Basics", Type: "Video", Category: "Animation", URL: "#", SortOrder: 6,
			Description: "A short walkthrough of storyboarding an explainer video."},
	}
	for i := range resources {
		resources[i].Status = "published"
		if err := s.CreateLibraryResource(ctx, &resources[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedShop(ctx context.Context) error {
	existing, err := s.ListProducts(ctx, ProductFilter{})
	if err != nil || len(existing) > 0 {
		return err
	}
	products := []Product{
		{Name: "Kuza Kizazi Sticker Pack", Category: "Merchandise", PriceCents: 50000, SortOrder: 1,
			Description: "A set of ten die-cut vinyl stickers featuring our signature marks.",
			Body:        "Durable, weatherproof vinyl stickers — perfect for laptops, water bottles, and notebooks."},
		{Name: "Branded Tote Bag", Category: "Merchandise", PriceCents: 120000, SortOrder: 2,
			Description: "A sturdy canvas tote with a clean screen-printed logo.",
			Body:        "Heavyweight natural canvas tote, screen printed by hand. Carries a laptop and then some."},
		{Name: "Creative Notebook", Category: "Merchandise", PriceCents: 80000, SortOrder: 3,
			Description: "A dotted-grid notebook for sketching ideas and planning projects.",
			Body:        "A5 dotted notebook with a soft-touch cover and lay-flat binding. 160 pages."},
		{Name: "Brand Identity Starter Kit", Category: "Digital Resources", PriceCents: 350000, SortOrder: 4,
			Description: "A downloadable template kit to define your brand's foundations.",
			Body:        "Editable templates for brand strategy, logo lockups, colour, and typography — a head start on your identity."},
		{Name: "Social Media Template Pack", Category: "Digital Resources", PriceCents: 200000, SortOrder: 5,
			Description: "Thirty editable social templates to keep your feed on-brand.",
			Body:        "A pack of thirty post and story templates, ready to customise with your brand's colours and fonts."},
		{Name: "Logo Design Workbook", Category: "Digital Resources", PriceCents: 150000, SortOrder: 6,
			Description: "A practical workbook for designing a logo that lasts.",
			Body:        "A guided digital workbook walking you through research, sketching, and refining a memorable logo."},
	}
	for i := range products {
		products[i].Status = "published"
		if err := s.CreateProduct(ctx, &products[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedAdmin(ctx context.Context, email, password string) error {
	adminRole, err := s.GetRoleByKey(ctx, "admin")
	if err != nil {
		return err
	}

	n, err := s.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		return s.CreateUser(ctx, &User{
			Email:        email,
			PasswordHash: string(hash),
			Name:         "Kuza Kizazi Admin",
			Role:         "admin",
			RoleID:       &adminRole.ID,
		})
	}

	// Back-fill role_id for any existing admin users created before 0008.
	_, err = s.pool.Exec(ctx, `
		UPDATE users SET role_id = $1 WHERE role = 'admin' AND role_id IS NULL`,
		adminRole.ID)
	return err
}

// seedRoles creates the built-in role catalog on first boot and keeps the
// admin role's permissions in sync on subsequent boots.
func (s *Store) seedRoles(ctx context.Context) error {
	for _, r := range BuiltinRoles() {
		if _, err := s.UpsertBuiltinRole(ctx, r.Key, r.Name, r.Description, r.Permissions); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedSettings(ctx context.Context) error {
	existing, err := s.GetSettings(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	return s.UpdateSettings(ctx, map[string]string{
		"site_name":          "Kuza Kizazi",
		"tagline":            "Creative agency · Nairobi",
		"footer_description": "Helping growing African brands build their identity, digital presence, and content systems through strategy, design, websites, media, and marketing.",
		"hero_title":         "We turn bold visions into digital reality.",
		"hero_subtitle":      "Kuza Kizazi is a Nairobi creative agency crafting brands, websites, and stories that move people.",
		"contact_email":      "info@kuzakizazi.com",
		"contact_phone":      "+254 745 357 116",
		"contact_address":    "Imenti House, Tom Mboya Street, Nairobi, Kenya",
		"social_facebook":    "https://facebook.com/kuzakizazi",
		"social_instagram":   "https://instagram.com/kuzakizazi",
		"social_twitter":     "https://twitter.com/kuzakizazi",
		"social_linkedin":    "https://linkedin.com/company/kuzakizazi",
	})
}

func (s *Store) seedStats(ctx context.Context) error {
	existing, err := s.ListStats(ctx)
	if err != nil || len(existing) > 0 {
		return err
	}
	stats := []Stat{
		{Label: "Projects Completed", Value: "66+", SortOrder: 1},
		{Label: "Happy Clients", Value: "472+", SortOrder: 2},
		{Label: "Countries Reached", Value: "18+", SortOrder: 3},
		{Label: "Years of Experience", Value: "7+", SortOrder: 4},
	}
	for i := range stats {
		if err := s.CreateStat(ctx, &stats[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedServices(ctx context.Context) error {
	existing, err := s.ListServices(ctx, false)
	if err != nil || len(existing) > 0 {
		return err
	}
	services := []Service{
		{Slug: "branding", Title: "Brand Identity", Icon: "✦", Pillar: "brand_identity", SortOrder: 1,
			Summary: "Clear identity systems that help growing brands look consistent, credible, and memorable.",
			Body:    "We shape the identity your audience recognises: strategy, logos, visual systems, and practical guidance your team can use consistently."},
		{Slug: "graphic-design", Title: "Graphic Design", Icon: "✎", Pillar: "brand_identity", SortOrder: 2,
			Summary: "Purposeful visual assets for campaigns, products, presentations, and everyday brand communication.",
			Body:    "We translate your message into polished visual work across print and digital touchpoints, keeping every piece recognisably on brand."},
		{Slug: "branded-merchandise", Title: "Branded Merchandise", Icon: "✧", Pillar: "brand_identity", SortOrder: 3,
			Summary: "Wearable and tangible brand designs that extend your identity beyond the screen.",
			Body:    "We design merchandise concepts and artwork that feel like a natural expression of your brand, ready for your chosen production partner."},
		{Slug: "web-development", Title: "Web Development", Icon: "⌘", Pillar: "digital_platforms", SortOrder: 4,
			Summary: "Fast, accessible websites and commerce experiences built around your business goals.",
			Body:    "We design and build websites that make your offer easy to understand, easy to use, and ready to support growth."},
		{Slug: "animation-video", Title: "Animation & Video", Icon: "▶", Pillar: "content_growth", SortOrder: 5,
			Summary: "Motion-led storytelling that explains ideas and gives your content more energy.",
			Body:    "From explainers to social motion, we develop animated and video content that communicates clearly and earns attention."},
		{Slug: "photography-videography", Title: "Photography & Videography", Icon: "◉", Pillar: "content_growth", SortOrder: 6,
			Summary: "Original photo and video content that captures your people, products, and moments.",
			Body:    "We plan and create brand-aligned visuals for social channels, launches, products, events, and campaigns."},
		{Slug: "online-presence-management", Title: "Content & Online Presence", Icon: "❖", Pillar: "content_growth", SortOrder: 7,
			Summary: "Consistent content systems that keep your brand useful, visible, and active online.",
			Body:    "We help you plan and manage valuable content, including editorial publishing and client-owned learning experiences."},
		{Slug: "digital-marketing", Title: "Digital Marketing", Icon: "↗", Pillar: "content_growth", SortOrder: 8,
			Summary: "Organic digital strategy focused on reaching the right audience and improving discoverability.",
			Body:    "We support your growth through social strategy and search optimisation built around clear goals and useful content."},
	}
	for i := range services {
		services[i].Status = "published"
		if err := s.CreateService(ctx, &services[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedProjects(ctx context.Context) error {
	existing, err := s.ListProjects(ctx, false)
	if err != nil || len(existing) > 0 {
		return err
	}
	projects := []Project{
		{Title: "TechNexus Platform", Client: "TechNexus Solutions", Category: "Platform Development", SortOrder: 1,
			Summary: "A custom SaaS dashboard for a fast-growing startup.",
			Body:    "We partnered with TechNexus to design and build a custom analytics dashboard — a clean, fast interface that turned a tangle of spreadsheets into a single source of truth for their team.",
			Results: "40% faster reporting, 3x user growth in six months."},
		{Title: "Clare Online Library", Client: "Clare", Category: "Web Development", SortOrder: 2,
			Summary: "An educational digital library platform for schools.",
			Body:    "Clare needed a digital library that students and teachers could actually enjoy using. We delivered a searchable, responsive platform with curated collections and reading tools.",
			Results: "12 partner schools onboarded in the first term."},
		{Title: "EcoVibe Brand Revamp", Client: "EcoVibe Lifestyle", Category: "Branding", SortOrder: 3,
			Summary: "A full visual identity redesign for a sustainable lifestyle brand.",
			Body:    "EcoVibe had outgrown its original look. We rebuilt the identity from the ground up — new logo, palette, packaging, and guidelines — to match its ambition.",
			Results: "Brand recognition up sharply after relaunch."},
	}
	for i := range projects {
		projects[i].Status = "published"
		if err := s.CreateProject(ctx, &projects[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedTeam(ctx context.Context) error {
	existing, err := s.ListTeam(ctx)
	if err != nil || len(existing) > 0 {
		return err
	}
	team := []TeamMember{
		{Name: "Ian Adams", Role: "Founder & Lead Strategist", SortOrder: 1,
			Bio:     "Ian founded Kuza Kizazi to help African brands tell their stories with confidence.",
			Socials: map[string]string{"linkedin": "https://linkedin.com"}},
		{Name: "Sarah Chen", Role: "Lead Designer", SortOrder: 2,
			Bio:     "Sarah leads design across branding and digital, obsessed with detail and clarity.",
			Socials: map[string]string{"dribbble": "https://dribbble.com"}},
		{Name: "Michael Okafor", Role: "Head of Engineering", SortOrder: 3,
			Bio:     "Michael builds the fast, reliable platforms behind every Kuza Kizazi project.",
			Socials: map[string]string{"linkedin": "https://linkedin.com"}},
	}
	for i := range team {
		if err := s.CreateTeamMember(ctx, &team[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedTestimonials(ctx context.Context) error {
	existing, err := s.ListTestimonials(ctx, false)
	if err != nil || len(existing) > 0 {
		return err
	}
	testimonials := []Testimonial{
		{Author: "Amina Yusuf", Role: "Marketing Director", Company: "TechNexus Solutions", SortOrder: 1,
			Quote: "Kuza Kizazi understood our product better than we did. The dashboard they built is the tool our whole team now lives in."},
		{Author: "David Mwangi", Role: "Founder", Company: "EcoVibe Lifestyle", SortOrder: 2,
			Quote: "The rebrand gave us a look that finally matches our ambition. Customers notice — and so do retailers."},
		{Author: "Grace Otieno", Role: "Head of School", Company: "Clare", SortOrder: 3,
			Quote: "Professional, patient, and genuinely creative. They delivered exactly what our students needed."},
	}
	for i := range testimonials {
		testimonials[i].Status = "published"
		if err := s.CreateTestimonial(ctx, &testimonials[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedBlog(ctx context.Context) error {
	cats, err := s.ListCategories(ctx)
	if err != nil {
		return err
	}
	if len(cats) == 0 {
		for _, name := range []string{"Design", "Strategy", "Technology", "Company News"} {
			c := &Category{Name: name}
			if err := s.CreateCategory(ctx, c); err != nil {
				return err
			}
			cats = append(cats, *c)
		}
	}

	posts, err := s.ListPosts(ctx, ListOptions{PerPage: 1})
	if err != nil {
		return err
	}
	if posts.Total > 0 {
		return nil
	}

	admin, err := s.GetUserByEmailOrNil(ctx)
	if err != nil {
		return err
	}

	catID := func(name string) *int64 {
		for i := range cats {
			if cats[i].Name == name {
				return &cats[i].ID
			}
		}
		return nil
	}

	samples := []Post{
		{Title: "Why Your Agency Needs a Custom CMS",
			Excerpt: "Off-the-shelf tools get you started — but a custom CMS is what lets your brand and your team move at full speed.",
			Content: "Generic solutions often hold back creative potential.\n\nWhen every page, product, and story has to be forced into someone else's template, your brand ends up looking like everyone else's. A custom content system flips that: the structure fits your content, not the other way around.\n\nIt also makes your team faster. Editors update the site without filing tickets, and developers ship features without fighting a plugin ecosystem.\n\nThat is exactly the thinking behind the platform powering this very site.",
			Status:  "published"},
		{Title: "The Future of Dynamic Design in 2026",
			Excerpt: "Design in 2026 is less about static layouts and more about systems that respond to people in real time.",
			Content: "The best digital experiences in 2026 feel alive.\n\nThey adapt to the viewer — their device, their context, even their intent — without ever feeling chaotic. Behind that flexibility is a strong design system: tokens, components, and rules that keep things consistent while letting them flex.\n\nFor brands, the lesson is simple. Invest in the system, not just the screen. Get the foundations right, and every future page comes faster and looks sharper.",
			Status:  "published"},
	}
	titleCat := map[string]string{
		"Why Your Agency Needs a Custom CMS":   "Technology",
		"The Future of Dynamic Design in 2026": "Design",
	}
	for i := range samples {
		samples[i].CategoryID = catID(titleCat[samples[i].Title])
		if admin != nil {
			samples[i].AuthorID = &admin.ID
		}
		if err := s.CreatePost(ctx, &samples[i]); err != nil {
			return err
		}
	}
	return nil
}
