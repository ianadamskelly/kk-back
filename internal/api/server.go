package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"kuzakizazi/internal/config"
	"kuzakizazi/internal/store"
)

// API holds shared dependencies for HTTP handlers.
type API struct {
	cfg    config.Config
	store  *store.Store
	mailer Mailer
}

// NewRouter builds the full HTTP handler tree.
func NewRouter(cfg config.Config, st *store.Store) http.Handler {
	a := &API{
		cfg:    cfg,
		store:  st,
		mailer: NewMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom, cfg.SMTPTLS),
	}

	// Per-IP buckets shared across the auth surface. login + register
	// are the two endpoints that touch credentials; both get rate-
	// limited so a single host can't sit on a wordlist all night.
	// loginLimiter is a touch more generous since legitimate users
	// fat-finger passwords; register stays tight to slow signup abuse.
	loginLimiter := newIPRateLimiter(20, 10)  // 20/min, burst 10
	registerLimiter := newIPRateLimiter(5, 3) // 5/min, burst 3
	// Public certificate verify/download — generous for genuine sharing
	// but enough to make code enumeration pointless (codes are ~50-bit
	// Crockford strings, so this is defence-in-depth).
	certLimiter := newIPRateLimiter(30, 15) // 30/min, burst 15

	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(corsMiddleware(cfg.CORSOrigin))

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", a.health)

		// --- Public endpoints ---
		r.Get("/posts", a.listPublicPosts)
		r.Get("/posts/{slug}", a.getPublicPost)
		r.Get("/posts/{slug}/comments", a.listComments)
		r.Post("/posts/{slug}/comments", a.createComment)
		r.Get("/categories", a.listCategories)

		r.Get("/services", a.listPublicServices)
		r.Get("/services/{slug}", a.getPublicService)
		r.Get("/projects", a.listPublicProjects)
		r.Get("/projects/{slug}", a.getPublicProject)
		r.Get("/team", a.listTeam)
		r.Get("/testimonials", a.listPublicTestimonials)
		r.Get("/stats", a.listStats)
		r.Get("/settings", a.getSettings)

		r.Get("/products", a.listPublicProducts)
		r.Get("/products/{slug}", a.getPublicProduct)
		r.Get("/products/{slug}/reviews", a.listPublicProductReviews)

		r.Get("/courses", a.listPublicCourses)
		r.Get("/courses/{slug}", a.getPublicCourse)
		r.Get("/courses/{slug}/reviews", a.listPublicCourseReviews)
		r.Get("/library", a.listPublicLibrary)
		r.Get("/library/{slug}", a.getPublicLibraryResource)

		r.Post("/contact", a.createSubmission)
		r.Post("/newsletter", a.subscribeNewsletter)
		r.Get("/unsubscribe/{token}", a.getUnsubscribePublic)
		r.Post("/unsubscribe/{token}", a.acceptUnsubscribePublic)
		r.Post("/orders", a.createOrder)
		r.Post("/coupons/validate", a.validateCoupon)

		// Payments. /payments/verify is hit by the gateway redirect
		// without our session, and /webhooks/flutterwave is auth'd by
		// signature — both stay public. /orders/{id}/pay moves into
		// the authenticated block below; the handler also enforces
		// order ownership.
		r.Get("/payments/verify", a.verifyPayment)
		r.Post("/webhooks/flutterwave", a.flutterwaveWebhook)

		// Signed downloads. The token in the URL is the only auth — it
		// encodes (user, order, file) and is verified inside the handler.
		// Lives outside the requireAuth group so the email link works
		// even after the cookie/session expires.
		r.Get("/downloads/{token}", a.downloadFile)

		// Signed file fetcher. The token authorises one read of one
		// protected file (library payload, course-task attachment).
		// Public so <a href> / <img src> work without an Authorization
		// header; the JWT inside the URL is the security boundary.
		r.Get("/files/{token}", a.servePublicFileToken)

		// Certificate verify + download — the code is the only auth.
		// Verify is a small JSON for the share-friendly page; download
		// streams the PDF straight to the browser.
		r.With(rateLimit(certLimiter)).Get("/cert/{code}", a.getPublicCertificate)
		r.With(rateLimit(certLimiter)).Get("/cert/{code}/download", a.downloadCertificate)

		// --- Authentication ---
		// Login + register are rate-limited per IP (see top of NewRouter).
		r.With(rateLimit(loginLimiter)).Post("/admin/login", a.login)
		r.With(rateLimit(loginLimiter)).Post("/auth/login", a.login)
		r.With(rateLimit(registerLimiter)).Post("/auth/register", a.register)
		// Logout clears the kk_session HttpOnly cookie. Public so a
		// stale-cookie caller can always sign themselves out. No rate
		// limit — being able to log out reliably matters.
		r.Post("/auth/logout", a.logout)

		// --- Account endpoints (any signed-in user) ---
		r.Group(func(r chi.Router) {
			r.Use(a.requireAuth)
			r.Get("/auth/me", a.me)
			// Payment initiation requires auth + the handler verifies
			// the caller owns the order. Was previously public, which
			// let anyone enumerate orders by id and read customer PII.
			r.Post("/orders/{id}/pay", a.initiatePayment)
			r.Get("/account/orders", a.listAccountOrders)
			r.Get("/memberships/me", a.getMyMembership)
			r.Post("/memberships/checkout", a.createMembershipCheckout)
			r.Post("/courses/{id}/checkout", a.createCourseCheckout)
			r.Get("/account/credit", a.getMyCredit)
			r.Get("/account/referrals", a.getMyReferrals)

			// Dashboard + profile.
			r.Get("/account/dashboard", a.getMyDashboard)
			r.Get("/account/profile", a.getMyProfile)
			r.Put("/account/profile", a.updateMyProfile)
			r.Post("/account/password", a.changeMyPassword)

			// Owned content.
			r.Get("/account/courses", a.listMyCourses)
			r.Get("/account/downloads", a.listMyDownloads)
			r.Get("/account/assets", a.listMyInteractiveAssets)
			r.Get("/account/assets/{assetId}", a.getMyInteractiveAsset)
			r.Post("/account/assets/{assetId}/export", a.exportMyInteractiveAsset)

			// Customer testimonials.
			r.Get("/account/testimonials", a.listMyTestimonials)
			r.Post("/account/testimonials", a.createMyTestimonial)

			// Reviews on products / courses (verified buyers only;
			// the handler enforces the purchase / enrolment check).
			r.Post("/account/reviews", a.upsertOwnReview)
			r.Delete("/account/reviews/{id}", a.deleteOwnReview)

			// Course tasks (read tasks + submissions for one course,
			// submit / re-submit a response).
			r.Get("/account/courses/{slug}/tasks", a.listMyCourseTasks)
			r.Post("/account/tasks/{taskId}/submit", a.submitCourseTask)
			// Mark a course finished — issues the completion certificate
			// when eligible (access + any required tasks passed).
			r.Post("/account/courses/{slug}/complete", a.completeCourse)
			// Student file upload for task attachments. Same backend as
			// the admin uploader (writes to ProtectedUploadDir, returns
			// "/files/<name>"); separate route so customers don't need
			// admin permissions to hit it.
			r.Post("/account/upload-file", a.uploadAccountFile)

			// Certificates earned by the signed-in customer.
			r.Get("/account/certificates", a.listMyCertificates)

			// Tickets (the "Complaints" tab).
			r.Get("/account/tickets", a.listMyTickets)
			r.Post("/account/tickets", a.createMyTicket)
			r.Get("/account/tickets/{id}", a.getMyTicket)
			r.Post("/account/tickets/{id}/messages", a.replyToMyTicket)
			r.Post("/account/tickets/{id}/close", a.closeMyTicket)
		})

		// --- Public invite endpoints (no auth) ---
		r.Get("/invitations/{token}", a.getInvitationPublic)
		r.Post("/invitations/{token}/accept", a.acceptInvitation)

		// --- Admin endpoints (JWT + role with at least one permission) ---
		r.Group(func(r chi.Router) {
			r.Use(a.requireAuth)
			r.Use(a.loadPermissions)
			r.Use(a.requireAnyPermission(store.AllPermissionKeys()...))

			// Identity + shared utilities (any staff user).
			r.Get("/admin/me", a.me)
			r.Post("/admin/upload", a.uploadImage)
			// Arbitrary non-image uploads (digital downloads, library
			// files, etc.). Image uploads keep using /admin/upload so
			// they're re-encoded as WebP.
			r.Post("/admin/upload-file", a.uploadFile)
			r.Get("/admin/permissions", a.listPermissions)

			// Posts.
			r.With(a.requirePermission("posts.view")).Get("/admin/posts", a.listAdminPosts)
			r.With(a.requirePermission("posts.view")).Get("/admin/posts/{id}", a.getAdminPost)
			r.With(a.requirePermission("posts.manage")).Post("/admin/posts", a.createPost)
			r.With(a.requirePermission("posts.manage")).Put("/admin/posts/{id}", a.updatePost)
			r.With(a.requirePermission("posts.manage")).Delete("/admin/posts/{id}", a.deletePost)

			// Categories.
			r.With(a.requirePermission("categories.manage")).Post("/admin/categories", a.createCategory)
			r.With(a.requirePermission("categories.manage")).Delete("/admin/categories/{id}", a.deleteCategory)

			// Comments.
			r.With(a.requirePermission("comments.view")).Get("/admin/comments", a.listAdminComments)
			r.With(a.requirePermission("comments.manage")).Delete("/admin/comments/{id}", a.deleteComment)

			// Reviews moderation (gated on comments.* since reviews are
			// the same shape of moderation work — keeps roles simple).
			r.With(a.requirePermission("comments.view")).Get("/admin/reviews", a.listAdminReviews)
			r.With(a.requirePermission("comments.manage")).Put("/admin/reviews/{id}/status", a.setAdminReviewStatus)
			r.With(a.requirePermission("comments.manage")).Delete("/admin/reviews/{id}", a.deleteAdminReview)

			// Services.
			r.With(a.requirePermission("services.view")).Get("/admin/services", a.listAdminServices)
			r.With(a.requirePermission("services.view")).Get("/admin/services/{id}", a.getAdminService)
			r.With(a.requirePermission("services.manage")).Post("/admin/services", a.createService)
			r.With(a.requirePermission("services.manage")).Put("/admin/services/{id}", a.updateService)
			r.With(a.requirePermission("services.manage")).Delete("/admin/services/{id}", a.deleteService)
			r.With(a.requirePermission("services.view")).Get("/admin/services/{id}/subservices", a.listAdminServiceSubservices)
			r.With(a.requirePermission("services.manage")).Post("/admin/services/{id}/subservices", a.createServiceSubservice)
			r.With(a.requirePermission("services.manage")).Put("/admin/services/{id}/subservices/{subserviceId}", a.updateServiceSubservice)
			r.With(a.requirePermission("services.manage")).Delete("/admin/services/{id}/subservices/{subserviceId}", a.deleteServiceSubservice)

			// Projects.
			r.With(a.requirePermission("projects.view")).Get("/admin/projects", a.listAdminProjects)
			r.With(a.requirePermission("projects.view")).Get("/admin/projects/{id}", a.getAdminProject)
			r.With(a.requirePermission("projects.manage")).Post("/admin/projects", a.createProject)
			r.With(a.requirePermission("projects.manage")).Put("/admin/projects/{id}", a.updateProject)
			r.With(a.requirePermission("projects.manage")).Delete("/admin/projects/{id}", a.deleteProject)

			// Team.
			r.With(a.requirePermission("team.view")).Get("/admin/team", a.listAdminTeam)
			r.With(a.requirePermission("team.view")).Get("/admin/team/{id}", a.getAdminTeamMember)
			r.With(a.requirePermission("team.manage")).Post("/admin/team", a.createTeamMember)
			r.With(a.requirePermission("team.manage")).Put("/admin/team/{id}", a.updateTeamMember)
			r.With(a.requirePermission("team.manage")).Delete("/admin/team/{id}", a.deleteTeamMember)

			// Testimonials.
			r.With(a.requirePermission("testimonials.view")).Get("/admin/testimonials", a.listAdminTestimonials)
			r.With(a.requirePermission("testimonials.view")).Get("/admin/testimonials/{id}", a.getAdminTestimonial)
			r.With(a.requirePermission("testimonials.manage")).Post("/admin/testimonials", a.createTestimonial)
			r.With(a.requirePermission("testimonials.manage")).Put("/admin/testimonials/{id}", a.updateTestimonial)
			r.With(a.requirePermission("testimonials.manage")).Delete("/admin/testimonials/{id}", a.deleteTestimonial)

			// Stats.
			r.With(a.requirePermission("stats.view")).Get("/admin/stats", a.listAdminStats)
			r.With(a.requirePermission("stats.manage")).Post("/admin/stats", a.createStat)
			r.With(a.requirePermission("stats.manage")).Put("/admin/stats/{id}", a.updateStat)
			r.With(a.requirePermission("stats.manage")).Delete("/admin/stats/{id}", a.deleteStat)

			// Settings.
			r.With(a.requirePermission("settings.view")).Get("/admin/settings", a.getSettings)
			r.With(a.requirePermission("settings.manage")).Put("/admin/settings", a.updateSettings)

			// Submissions.
			r.With(a.requirePermission("submissions.view")).Get("/admin/submissions", a.listSubmissions)
			r.With(a.requirePermission("submissions.manage")).Put("/admin/submissions/{id}", a.updateSubmission)
			r.With(a.requirePermission("submissions.manage")).Delete("/admin/submissions/{id}", a.deleteSubmission)

			// Subscribers.
			r.With(a.requirePermission("subscribers.view")).Get("/admin/subscribers", a.listSubscribers)
			r.With(a.requirePermission("subscribers.manage")).Delete("/admin/subscribers/{id}", a.deleteSubscriber)

			// Products.
			r.With(a.requirePermission("products.view")).Get("/admin/products", a.listAdminProducts)
			r.With(a.requirePermission("products.view")).Get("/admin/products/{id}", a.getAdminProduct)
			r.With(a.requirePermission("products.manage")).Post("/admin/products", a.createProduct)
			r.With(a.requirePermission("products.manage")).Put("/admin/products/{id}", a.updateProduct)
			r.With(a.requirePermission("products.manage")).Delete("/admin/products/{id}", a.deleteProduct)
			// Product image gallery: attach already-uploaded image URLs
			// (from POST /api/admin/upload), reorder them, mark cover.
			r.With(a.requirePermission("products.view")).Get("/admin/products/{id}/images", a.listProductImages)
			r.With(a.requirePermission("products.manage")).Post("/admin/products/{id}/images", a.addProductImage)
			r.With(a.requirePermission("products.manage")).Put("/admin/products/{id}/images/order", a.reorderProductImages)
			r.With(a.requirePermission("products.manage")).Put("/admin/products/{id}/images/{imageId}/cover", a.setProductCoverImage)
			r.With(a.requirePermission("products.manage")).Delete("/admin/products/{id}/images/{imageId}", a.deleteProductImage)
			// Digital downloads: attach files (uploaded via
			// /admin/upload-file) to a product. Never exposed publicly;
			// customer access flows through signed tokens.
			r.With(a.requirePermission("products.view")).Get("/admin/products/{id}/downloads", a.listProductDownloads)
			r.With(a.requirePermission("products.manage")).Post("/admin/products/{id}/downloads", a.addProductDownload)
			r.With(a.requirePermission("products.manage")).Put("/admin/products/{id}/downloads/order", a.reorderProductDownloads)
			r.With(a.requirePermission("products.manage")).Delete("/admin/products/{id}/downloads/{downloadId}", a.deleteProductDownload)

			// Orders.
			r.With(a.requirePermission("orders.view")).Get("/admin/orders", a.listOrders)
			r.With(a.requirePermission("orders.view")).Get("/admin/orders/{id}", a.getOrder)
			r.With(a.requirePermission("orders.manage")).Put("/admin/orders/{id}", a.updateOrder)
			r.With(a.requirePermission("orders.manage")).Delete("/admin/orders/{id}", a.deleteOrder)

			// Courses + lessons.
			r.With(a.requirePermission("courses.view")).Get("/admin/courses", a.listAdminCourses)
			r.With(a.requirePermission("courses.view")).Get("/admin/courses/{id}", a.getAdminCourse)
			r.With(a.requirePermission("courses.view")).Get("/admin/courses/{id}/lessons", a.listCourseLessons)
			r.With(a.requirePermission("courses.view")).Get("/admin/lessons/{id}", a.getLesson)
			r.With(a.requirePermission("courses.manage")).Post("/admin/courses", a.createCourse)
			r.With(a.requirePermission("courses.manage")).Put("/admin/courses/{id}", a.updateCourse)
			r.With(a.requirePermission("courses.manage")).Delete("/admin/courses/{id}", a.deleteCourse)
			r.With(a.requirePermission("courses.manage")).Post("/admin/courses/{id}/lessons", a.createLesson)
			r.With(a.requirePermission("courses.manage")).Put("/admin/courses/{id}/lessons/reorder", a.reorderLessons)
			r.With(a.requirePermission("courses.manage")).Put("/admin/lessons/{id}", a.updateLesson)
			r.With(a.requirePermission("courses.manage")).Delete("/admin/lessons/{id}", a.deleteLesson)

			// Resources attached to a course or to one of its lessons.
			// lessonId on POST is optional — null = course-wide.
			r.With(a.requirePermission("courses.view")).Get("/admin/courses/{id}/resources", a.listCourseResources)
			r.With(a.requirePermission("courses.manage")).Post("/admin/courses/{id}/resources", a.addCourseResource)
			r.With(a.requirePermission("courses.manage")).Delete("/admin/courses/{id}/resources/{resourceId}", a.deleteCourseResource)

			// Module-end tasks + the student submissions inbox.
			r.With(a.requirePermission("courses.view")).Get("/admin/courses/{id}/tasks", a.listAdminCourseTasks)
			r.With(a.requirePermission("courses.manage")).Post("/admin/courses/{id}/tasks", a.createCourseTask)
			r.With(a.requirePermission("courses.manage")).Put("/admin/courses/{id}/tasks/{taskId}", a.updateCourseTask)
			r.With(a.requirePermission("courses.manage")).Delete("/admin/courses/{id}/tasks/{taskId}", a.deleteCourseTask)
			r.With(a.requirePermission("courses.view")).Get("/admin/courses/{id}/submissions", a.listAdminCourseSubmissions)
			// Global grading inbox: every course's submissions in one
			// feed. Distinct path from the contact-message
			// /admin/submissions above (which lists ContactSubmissions).
			r.With(a.requirePermission("courses.view")).Get("/admin/course-submissions", a.listAllAdminSubmissions)
			r.With(a.requirePermission("courses.manage")).Put("/admin/submissions/{submissionId}/grade", a.gradeSubmission)
			// Manual cert issuance from the admin (e.g. for courses
			// without required-pass tasks where there's no automatic
			// trigger). Idempotent — re-running returns the existing
			// row instead of minting a fresh code.
			r.With(a.requirePermission("courses.manage")).Post("/admin/courses/{id}/certificates", a.issueAdminCertificate)

			// Library.
			r.With(a.requirePermission("library.view")).Get("/admin/library", a.listAdminLibrary)
			r.With(a.requirePermission("library.view")).Get("/admin/library/{id}", a.getAdminLibraryResource)
			r.With(a.requirePermission("library.manage")).Post("/admin/library", a.createLibraryResource)
			r.With(a.requirePermission("library.manage")).Put("/admin/library/{id}", a.updateLibraryResource)
			r.With(a.requirePermission("library.manage")).Delete("/admin/library/{id}", a.deleteLibraryResource)

			// Revenue + service income + memberships.
			r.With(a.requireAnyPermission("orders.view", "service_revenue.view", "memberships.view")).Get("/admin/revenue", a.adminRevenueSummary)
			r.With(a.requirePermission("memberships.view")).Get("/admin/memberships", a.listAdminMemberships)
			r.With(a.requirePermission("service_revenue.view")).Get("/admin/service-revenue", a.listAdminServiceRevenue)
			r.With(a.requirePermission("service_revenue.manage")).Post("/admin/service-revenue", a.createAdminServiceRevenue)
			r.With(a.requirePermission("service_revenue.manage")).Delete("/admin/service-revenue/{id}", a.deleteAdminServiceRevenue)

			// Roles + permissions.
			r.With(a.requirePermission("roles.view")).Get("/admin/roles", a.listAdminRoles)
			r.With(a.requirePermission("roles.manage")).Post("/admin/roles", a.createAdminRole)
			r.With(a.requirePermission("roles.view")).Get("/admin/roles/{id}", a.getAdminRole)
			r.With(a.requirePermission("roles.manage")).Put("/admin/roles/{id}", a.updateAdminRole)
			r.With(a.requirePermission("roles.manage")).Delete("/admin/roles/{id}", a.deleteAdminRole)

			// Staff users.
			r.With(a.requirePermission("users.view")).Get("/admin/users", a.listAdminUsers)
			r.With(a.requirePermission("users.manage")).Put("/admin/users/{id}", a.updateAdminUser)
			r.With(a.requirePermission("users.manage")).Delete("/admin/users/{id}", a.deleteAdminUser)

			// Invitations.
			r.With(a.requirePermission("users.view")).Get("/admin/invitations", a.listAdminInvitations)
			r.With(a.requireAnyPermission("users.invite", "users.manage")).Post("/admin/invitations", a.createAdminInvitation)
			r.With(a.requirePermission("users.manage")).Delete("/admin/invitations/{id}", a.deleteAdminInvitation)

			// Coupons.
			r.With(a.requirePermission("coupons.view")).Get("/admin/coupons", a.listAdminCoupons)
			r.With(a.requirePermission("coupons.manage")).Post("/admin/coupons", a.createAdminCoupon)
			r.With(a.requirePermission("coupons.manage")).Put("/admin/coupons/{id}", a.updateAdminCoupon)
			r.With(a.requirePermission("coupons.manage")).Delete("/admin/coupons/{id}", a.deleteAdminCoupon)

			// Rewards (referrals + credit overview).
			r.With(a.requirePermission("rewards.view")).Get("/admin/rewards", a.adminRewardsOverview)

			// Tickets (customer-raised "complaints").
			r.With(a.requirePermission("tickets.view")).Get("/admin/tickets", a.listAdminTickets)
			r.With(a.requirePermission("tickets.view")).Get("/admin/tickets/{id}", a.getAdminTicket)
			r.With(a.requirePermission("tickets.manage")).Post("/admin/tickets/{id}/messages", a.replyToAdminTicket)
			r.With(a.requirePermission("tickets.manage")).Put("/admin/tickets/{id}", a.updateAdminTicket)

			// Newsletters.
			r.With(a.requirePermission("newsletters.view")).Get("/admin/newsletters", a.listAdminNewsletters)
			r.With(a.requirePermission("newsletters.view")).Get("/admin/newsletters/{id}", a.getAdminNewsletter)
			r.With(a.requirePermission("newsletters.manage")).Post("/admin/newsletters", a.createAdminNewsletter)
			r.With(a.requirePermission("newsletters.manage")).Put("/admin/newsletters/{id}", a.updateAdminNewsletter)
			r.With(a.requirePermission("newsletters.manage")).Delete("/admin/newsletters/{id}", a.deleteAdminNewsletter)
			r.With(a.requirePermission("newsletters.manage")).Post("/admin/newsletters/{id}/send", a.sendAdminNewsletter)
			r.With(a.requirePermission("newsletters.view")).Get("/admin/subscribers/tag-stats", a.listSubscriberTagStats)
		})
	})

	// Keep the historical nested protected path dark during deployment
	// rollouts where an old volume may still contain payloads there.
	r.Handle("/uploads/protected", http.NotFoundHandler())
	r.Handle("/uploads/protected/*", http.NotFoundHandler())

	// Serve public uploaded images only.
	uploads := http.FileServer(http.Dir(cfg.UploadDir))
	r.Handle("/uploads/*", http.StripPrefix("/uploads/", uploads))

	return r
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
