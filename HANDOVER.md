# Kuza Kizazi — Handover

A single document covering the whole stack: what it is, how it runs,
what's been built, what's deferred, and where the bodies are buried.
Aimed at any developer (including a future you) picking the project
up cold.

---

## 1. What it is

Kuza Kizazi is a Nairobi creative-agency website with a public-
facing marketing site (services, portfolio, insights blog), a digital
storefront (physical + digital products, multi-gateway checkout), a
self-paced course platform (member-gated lessons, module tasks,
completion certificates), a member library, and a full admin CMS
that runs everything end-to-end.

The whole thing runs as two services behind a single Coolify host.

## 2. Repositories

| Repo | URL | What's in it |
| --- | --- | --- |
| Backend | <https://github.com/ianadamskelly/kk-back> | Go HTTP API, Postgres schema migrations, all business logic |
| Frontend | <https://github.com/ianadamskelly/kk-front> | Next.js 16 App-Router site, both public pages and admin SPA |

Hosting: a single [Coolify](https://coolify.io/) instance runs both
containers + a managed Postgres + separate persistent volumes for
public and protected uploads.

## 3. Architecture at a glance

```
┌───────────────────────────────────────────────────────────────────┐
│ kuzakizazi.com  (Next.js 16, SSR + client)                         │
│   • Public marketing site, shop, courses, library, account area    │
│   • Admin SPA under /admin                                         │
└──────────────────┬────────────────────────────────────────────────┘
                   │ HTTPS, cookies span both subdomains
                   ▼
┌───────────────────────────────────────────────────────────────────┐
│ api.kuzakizazi.com  (Go 1.25, chi router)                          │
│   • /api/*  business logic, auth, payments, file gating            │
│   • /uploads/*  public file server (cover images only)             │
│   • /api/files/{token}  signed-token gate for protected payloads   │
└────────┬──────────────────────────────────┬───────────────────────┘
         │                                  │
         ▼                                  ▼
   PostgreSQL 14+                  Persistent volumes
   (Coolify managed)               /app/uploads (public)
                                   /app/protected_uploads (protected)
```

Auth is JWT-in-HttpOnly-cookie (`kk_session`), spanning the two
subdomains via `Domain=.kuzakizazi.com`. Customers and admins share
the same cookie name; the role lives inside the JWT.

## 4. Tech stack

**Backend** (Go 1.25, single binary)
- `github.com/go-chi/chi/v5` — router
- `github.com/jackc/pgx/v5` — Postgres driver + connection pool
- `github.com/golang-jwt/jwt/v5` — sessions + signed download/file tokens
- `golang.org/x/crypto/bcrypt` — password hashing
- `github.com/HugoSmits86/nativewebp` + `golang.org/x/image/webp` — image upload re-encoding
- `github.com/go-pdf/fpdf` — certificate PDF generation
- `github.com/microcosm-cc/bluemonday` — HTML sanitisation on stored rich text
- `golang.org/x/time/rate` — per-IP rate limiting on auth endpoints
- `github.com/joho/godotenv` — dev-mode .env loading

**Frontend** (Next.js 16, React 19)
- App Router, Turbopack for dev, standalone output for prod Docker
- Tailwind v4 (with the `@theme` brand palette in `app/globals.css`)
- TipTap v3 (StarterKit + Link + Image + Table extensions) — admin rich-text editor

**Infra**
- Postgres 14+ (Coolify-managed resource)
- Coolify on a single VPS
- SMTP via the credentials configured in the backend env

## 5. Running locally

You can run either service standalone. The backend talks to a real
Postgres; the frontend talks to whatever `NEXT_PUBLIC_API_URL` says.

### Backend

```bash
cd backend
cp .env.example .env             # then edit DATABASE_URL, JWT_SECRET, etc.
createdb kuzakizazi              # or any existing Postgres database
go mod download
go run .                         # listens on :8080
```

On boot the server runs every pending migration under
`internal/store/migrations/` (numbered SQL files, idempotent),
seeds an admin user from `SEED_ADMIN_*`, and starts logging at
`http://localhost:8080`.

### Frontend

```bash
cd frontend
npm install
echo "NEXT_PUBLIC_API_URL=http://localhost:8080" > .env.local
echo "NEXT_PUBLIC_SITE_URL=http://localhost:3000" >> .env.local
npm run dev                      # listens on :3000
```

Public site at <http://localhost:3000>, admin at
<http://localhost:3000/admin>, login with `admin@kuzakizazi.com` /
`admin123` (the seed defaults).

## 6. Environment variables

### Backend

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port. |
| `DATABASE_URL` | (local dev) | Postgres connection string. |
| `JWT_SECRET` | dev placeholder — **change in prod** | Signs every JWT (auth sessions, signed download/file tokens). Long random string. |
| `SEED_ADMIN_EMAIL` / `SEED_ADMIN_PASSWORD` | dev defaults — **change in prod** | First admin created on first boot. Rotate the password after first login. |
| `UPLOAD_DIR` | `uploads` | Where **public** uploads go (cover images). Served at `/uploads/*`. |
| `PROTECTED_UPLOAD_DIR` | `protected_uploads` | Where **protected** uploads go (digital downloads, course resources, member library files, course-task attachments). Must be outside `UPLOAD_DIR`; only token endpoints read it. |
| `CORS_ORIGIN` | `http://localhost:3000` | Exact frontend origin. **Cannot be `*`** because cookies require Allow-Credentials. |
| `COOKIE_DOMAIN` | `""` | Parent domain for the `kk_session` cookie. Set to `.kuzakizazi.com` in prod so it spans subdomains. Blank in dev. |
| `COOKIE_SECURE` | `false` | Forces the `Secure` flag on cookies. `true` in prod (HTTPS). |
| `PUBLIC_BASE_URL` | `http://localhost:3000` | Frontend URL. Embedded in emails (verify URLs, order links). |
| `API_PUBLIC_URL` | `http://localhost:8080` | Backend URL. Embedded in emails (download links, cert PDF download). |
| `PAYMENT_CURRENCY` | `KES` | Default checkout currency. |

Payments (optional, both blank disables online payment):

| Variable | Notes |
| --- | --- |
| `FLUTTERWAVE_PUBLIC_KEY`, `FLUTTERWAVE_SECRET_KEY`, `FLUTTERWAVE_SECRET_HASH` | Flutterwave credentials. |
| `FLUTTERWAVE_BASE_URL` | Override Flutterwave API base. |
| `SIFALOPAY_API_USER`, `SIFALOPAY_API_KEY` | Sifalopay credentials. |
| `SIFALOPAY_BASE_URL`, `SIFALOPAY_VERIFY_URL`, `SIFALOPAY_CHECKOUT_URL` | Override Sifalopay endpoints. |
| `KES_PER_USD` | FX rate for KES → USD on Sifalopay. Default `130`. |

SMTP (optional, blank = disabled). Both `SMTP_*` and `MAIL_*` names work:

| Variable | Notes |
| --- | --- |
| `SMTP_HOST` / `MAIL_HOST` | SMTP server. |
| `SMTP_PORT` / `MAIL_PORT` | Defaults `587`. |
| `SMTP_USER` / `MAIL_USERNAME` | Auth username. |
| `SMTP_PASS` / `MAIL_PASSWORD` | Auth password. |
| `SMTP_FROM` / `MAIL_FROM_ADDRESS` | From header. |
| `SMTP_TLS` | `true`/`false`. Inferred when port is `465` or `MAIL_SCHEME=smtps`. |

### Frontend

| Variable | Purpose |
| --- | --- |
| `NEXT_PUBLIC_API_URL` | Backend base URL. Baked at **build** time. |
| `NEXT_PUBLIC_SITE_URL` | Frontend base URL. Used for sitemap, robots, canonical, OG tags. Baked at build time. |

Both are `NEXT_PUBLIC_*` so they're embedded in the client bundle —
they must be set as **build-time** variables in Coolify (not
runtime env).

## 7. Production deployment (Coolify)

The Coolify project has three resources:

1. **Postgres** — managed resource; copy its internal connection
   string into the backend's `DATABASE_URL`.
2. **Backend service** — built from `kk-back` repo, Dockerfile
   pack. Domain `api.kuzakizazi.com`. Health check `/api/health`.
   Persistent volumes mounted at `/app/uploads` for public assets and
   `/app/protected_uploads` for protected payloads.
3. **Frontend service** — built from `kk-front` repo, Dockerfile
   pack. Domain `kuzakizazi.com` (+ `www` redirect). Build
   variables for `NEXT_PUBLIC_*`.

Auto-deploy is wired via the webhook on each service so pushes to
`main` redeploy automatically.

### First-time setup checklist (or after `git clone`-style move)

1. DNS A records for the apex, `www`, and `api` subdomains → the
   server IP.
2. Set the Postgres `DATABASE_URL` in the backend's env.
3. Set every required backend env var (`JWT_SECRET`, `CORS_ORIGIN`,
   `PUBLIC_BASE_URL`, `API_PUBLIC_URL`, `COOKIE_DOMAIN=.kuzakizazi.com`,
   `COOKIE_SECURE=true`, payment creds, SMTP creds).
4. Set frontend build vars (`NEXT_PUBLIC_API_URL`,
   `NEXT_PUBLIC_SITE_URL`).
5. Mount persistent volumes at `/app/uploads` and `/app/protected_uploads`.
6. Deploy both services. Watch the backend log — first boot runs
   every migration and seeds the admin.
7. Log in, change `SEED_ADMIN_PASSWORD` immediately.

## 8. Features in production

### Public site (`kuzakizazi.com`)
- Home, services, portfolio, insights blog, courses, shop,
  member library, membership, contact, privacy, terms
- Auto-scrolling testimonials marquee on home
- Site-wide JSON-LD (Organization on home, Article on insight
  detail, Product on shop, Course on course)
- Dynamic sitemap.xml + robots.txt + manifest.webmanifest
- Inline signup at checkout (no separate "create account" step)
- Reviews on products + courses, gated to verified buyers
- Scroll-back-to-top button site-wide
- Apple touch icon + SVG favicon

### Customer account (`/account`)
- Dashboard, orders, downloads (digital products with per-file
  download tokens + per-customer download limits), courses,
  certificates, profile + password change, support tickets,
  testimonials submission, referrals + store credit

### Course experience (`/courses`)
- Left filter rail (level / category / price tier)
- Per-lesson + per-course resources (URLs or uploaded files)
- Module-end tasks (admin-graded, optional pass-gating)
- PDF completion certificates with verify endpoint and email
- Dark theme on lesson pages (preference persists in
  localStorage; defaults to system)

### Admin CMS (`/admin`)
- Posts, products, courses, services, projects, library,
  team, testimonials, comments, reviews, orders, memberships,
  coupons, rewards, roles, users, settings, tickets,
  newsletters, contact submissions, course submissions
- Course wizard with curriculum + resources + tasks
- Product gallery (multi-image, drag reorder, cover) + digital
  downloadable files
- Library entries support either URL or file upload
- Status as a two-state toggle (Draft ⇄ Published)
- Sticky top bar, independent sidebar + content scroll
- Rich text editor: TipTap with tables, image upload, source view
- Dark theme

### Commerce
- Flutterwave (card / M-Pesa / mobile money) + Sifalopay
  (eDahab / EVC / Zaad), inline + redirect modes
- Coupons (codes + per-user redemption tracking)
- Store credit (referral-driven, redeemable at checkout)
- Coupons and store credit held at checkout, consumed once on
  confirmation, and released on abandoned or failed payments
- Order emails: confirmation on placement, fulfilment on admin
  mark-as-fulfilled (or auto for digital-only orders)
- Multi-currency: KES native, USD via configurable FX rate

## 9. Authentication model

All auth uses JWT (HS256 with `JWT_SECRET`) carried in an HttpOnly
`kk_session` cookie. Domain `.kuzakizazi.com` so the same cookie
serves the frontend and the api subdomain. `SameSite=Lax` blocks
cross-site CSRF; `Secure` enforces HTTPS in prod.

The backend's `claimsFromRequest` helper tries the `Authorization:
Bearer` header first and falls through to the cookie — this kept
the migration backward-compatible. Both code paths agree on
validity via the shared `parseClaims` verifier.

`role` inside the JWT discriminates customer vs admin. The customer
context filters to `role === "customer"` so an admin cookie on a
customer page just shows "anonymous" (correctly).

Admin permissions live on the user's `role_id` → role permissions
junction; permission checks happen via `requirePermission(key)` or
`requireAnyPermission(keys...)` chi middleware.

## 10. Security posture

### Hardened (commit history under "Security:" tag)
- **HttpOnly cookies** for both customer and admin sessions
  (closes localStorage-token-readable-via-XSS)
- **Stored XSS sanitisation** via bluemonday on every rich-text
  field (posts, products, courses, services, projects, library,
  comments, reviews, testimonials, lessons)
- **Order ownership** enforced on `POST /api/orders/{id}/pay`
- **Discount reservations** hold coupon and store-credit value at
  checkout and consume it atomically with first confirmation
- **Protected file storage** — digital downloads, member library
  files, course task attachments all live outside the public
  `/uploads/*` namespace; access only via signed `/api/files/{token}`
  (1h TTL) or `/api/downloads/{token}` (7d TTL, with download
  counter enforcement)
- **Course task submissions** require enrollment in the course
- **Per-IP rate limiting** on `/auth/login` (20/min, burst 10),
  `/auth/register` (5/min, burst 3), respects X-Forwarded-For only
  when the connection is from a loopback (Coolify+Traefik pattern)
- **Path traversal** defence on every file-serving handler
- **CORS Allow-Credentials** with a specific origin (never `*`)

### Deferred / known limitations
- **No CSRF token plumbing** — relies on SameSite=Lax + CORS
  origin gate. Sufficient for the SPA pattern but worth revisiting
  if we ever embed admin UI in a third-party context.
- **Rate limits are in-memory + per-instance**. If we ever scale
  horizontally, swap for Redis-backed buckets.
- **Phase 5: help chat** with RAG over site content is intentionally
  deferred (user choice).
- **Admin pages still render some unsanitised HTML** in places
  (mostly preview tiles for admin content the admin themselves
  typed). Low risk because admins are trusted, but worth a pass
  someday.

## 11. Database

Schema lives in numbered SQL files under
`backend/internal/store/migrations/`. The migration runner
(`migrate.go`) embeds them with `//go:embed`, tracks applied
versions in `schema_migrations`, and runs any pending on boot.

Adding a migration: create `NNNN_short_description.sql` with the
next number, write idempotent SQL (`IF NOT EXISTS`, `ON CONFLICT
DO NOTHING`), push. The boot logs will show it apply.

### Important tables

| Table | Notes |
| --- | --- |
| `users` | Auth + profile. `role` is `customer` or `admin`. |
| `roles` + `role_permissions` | RBAC for admin users. |
| `posts`, `categories` | Blog. |
| `services`, `projects` | Marketing pages. |
| `products`, `product_images`, `product_downloads` | Shop catalogue + multi-image gallery + digital files. |
| `orders`, `order_items` | Customer purchases. `kind` ∈ `shop`/`course`/`membership`. Status includes `payment_review` for paid orders requiring reconciliation. |
| `payments` | Per-gateway transaction record. |
| `coupons`, `coupon_redemptions` | Promo codes. |
| `credit_transactions` | Store credit ledger. |
| `order_discount_reservations` | Checkout holds for coupon and store-credit value. |
| `courses`, `lessons`, `course_resources`, `course_tasks`, `course_task_submissions`, `course_module_progress`, `certificates` | LMS. |
| `memberships` | Active subscriptions. |
| `library_resources` | Member library catalogue. |
| `testimonials`, `comments`, `reviews` | UGC + moderation. |
| `contact_submissions`, `tickets`, `ticket_messages` | Support / contact. |
| `newsletters`, `subscribers`, `subscriber_tags` | Mailing list. |
| `product_download_grants` | Per-(user, order, file) download counter. |

### Backup

Coolify has a backup tab on the Postgres resource — enable nightly
S3 or local backups. The uploads volume should be backed up
separately (Coolify volume backup or rsync to off-site).

## 12. File storage

Two physical directories on separate persistent volumes:

- `UploadDir` (default `/app/uploads`) — public. The static file
  handler serves anything in here at `/uploads/<name>`. Used for
  product cover images, library cover images, post cover images,
  team avatars. Anything that should be linkable from anywhere.

- `ProtectedUploadDir` (default `/app/protected_uploads`) — never
  served publicly. Holds digital product payloads, member library
  files, course task attachments. Reads go through two endpoints:
  - `GET /api/files/{token}` — short-lived (1h) signed token, used
    for library + submission previews.
  - `GET /api/downloads/{token}` — long-lived (7d) signed token,
    used for product downloads with counter enforcement.

The signed-token URLs are minted inside the handler that returns
the listing — so the URL the client sees is already
`/api/files/<jwt>` (or `/api/downloads/<jwt>`), never the raw
`/files/<name>`.

A startup task (`api.MigrateLegacyProtectedFiles`) reissues legacy
protected references, including course resources and image payloads,
under fresh names in the protected directory. Startup fails if an
exposed referenced payload cannot be migrated safely.

## 13. Email

`internal/api/mailer.go` implements an SMTPMailer that sends:

- Welcome email on customer signup
- Invitation email when an admin invites a teammate
- Order confirmation on placement
- Order fulfilled email (with download links for digital orders)
- Certificate issued email (with PDF download link)
- Newsletter blasts (personalised unsubscribe URL per recipient)

All sends are async + best-effort. SMTP failures log but never
block the user-facing action.

Templates are inline Go string concatenation — replace with a
template library later if you want richer designs.

## 14. Payments

Two gateways live behind `/api/orders/{id}/pay?gateway=...`:

- **Flutterwave** — inline checkout (modal on the page) via the
  Flutterwave Inline JS SDK. Supports KES + USD natively (USD
  conversion via the gateway).
- **Sifalopay** — redirect to their hosted checkout. USD-only
  internally; KES totals are converted via `KES_PER_USD` env.

Verification:
- Flutterwave: the user is redirected back to `/payment/complete`
  which calls `/api/payments/verify?tx_ref=...`.
- Sifalopay: same redirect/verify pattern with `?gateway=sifalo`.

On successful verify:
1. Payment row marked `successful`.
2. Order status → `confirmed`.
3. `applyEntitlements` grants course access, membership, etc.
4. A held discount reservation is consumed atomically with first
   confirmation; replayed callbacks cannot charge or grant twice.
5. `autoFulfilDigitalOrder` checks if every line item is digital —
   if so, status → `fulfilled` immediately and the fulfilment
   email fires with download links.

## 15. Operations

### Health check

`GET /api/health` returns 200 with a tiny JSON. Coolify's health
check is wired to this; failures roll back the deploy.

### Logs

Coolify → service → Logs tab. Filter for `/api/admin/upload` to
see recent uploads, etc. Backend logs every request (chi's logger
middleware) plus structured app-level lines for emails, migrations,
cert issuance, etc.

### Common ops

| Task | How |
| --- | --- |
| Add an admin | Send an invite from `/admin/team` (or `/admin/users`). They get an email with a magic accept link; on accept they're auto-signed-in. |
| Rotate JWT secret | Change `JWT_SECRET` in Coolify env, redeploy. All sessions invalidate (everyone re-logs in). |
| Reset an admin password | If you can log in: `/account/profile`. If locked out: edit the `users` row directly (set `password_hash` to a bcrypt of the new password) — there's no admin password-reset flow yet. |
| See current orders | `/admin/orders` or query `SELECT * FROM orders ORDER BY created_at DESC LIMIT 50;`. |
| Issue a certificate manually | `/admin/courses/{id}/submissions` → "🎓 Issue certificate" on the row. Or hit `POST /api/admin/courses/{id}/certificates` with `{userId: N}`. |
| Refund a paid order | No UI button yet — set status to `cancelled` via `/admin/orders/{id}`, then handle the actual refund out-of-band with the gateway. |

## 16. Code map

### Backend (`kk-back`)

```
main.go                       — entry: load env, open DB, seed, mount router
internal/config/              — env-driven Config struct
internal/store/               — Postgres data access; one file per concern
internal/store/migrations/    — numbered SQL files, embedded
internal/api/                 — HTTP handlers + middleware
  server.go                   — chi router + all route bindings
  auth.go                     — login/register, JWT, cookies, middleware
  middleware.go               — CORS
  ratelimit.go                — per-IP token buckets
  sanitize.go                 — bluemonday policy
  protected_files.go          — /api/files/{token}
  downloads.go                — /api/downloads/{token} (product downloads)
  certificates.go             — PDF cert generation
  order_emails.go             — order email composition + auto-fulfil
  upload.go                   — image + arbitrary file uploads
  mailer.go                   — SMTP mailer + templates
  payments.go, flutterwave.go, sifalo.go  — payment flows
  (one file per resource: posts, products, courses, ...)
```

### Frontend (`kk-front`)

```
app/                          — Next.js App Router
  layout.tsx                  — root layout, metadata, OG defaults
  icon.svg, apple-icon.tsx    — favicon + iOS touch icon
  sitemap.ts, robots.ts, manifest.ts   — Next 16 metadata file conventions
  (public)/                   — public site routes
  admin/                      — admin SPA routes
  cert/[code]/                — public certificate verify page
lib/
  api.ts                      — fetch helpers + types
  customer.tsx                — customer auth context + customerFetch
  adminSession.tsx            — admin auth context
  auth.ts                     — legacy admin token helpers (now no-ops)
  cart.tsx                    — shopping cart context
  payments.ts                 — Flutterwave/Sifalo handoff
  theme.tsx, progress.ts, readtime.ts  — UX helpers
components/                   — shared UI components
components/admin/             — admin-only UI components
```

## 17. Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| Login appears to succeed but admin pages immediately bounce to login | `COOKIE_DOMAIN` mismatch, or `Secure` cookie sent over HTTP | Verify `COOKIE_DOMAIN=.kuzakizazi.com` and `COOKIE_SECURE=true` in prod, and that HTTPS is actually live. |
| CORS error on every fetch | `CORS_ORIGIN` is `*` or mismatched | Set to exactly `https://kuzakizazi.com` (no trailing slash). Credentials require a specific origin. |
| Uploads succeed but downloads 404 | Volume mount missing or wrong path | Check that `/app/uploads` and `/app/protected_uploads` are both persistently mounted; check `UPLOAD_DIR` + `PROTECTED_UPLOAD_DIR` env. |
| `Could not save file` on upload | File ownership inside container (volume is root-owned but the binary runs as `app`) | Ensure both upload mounts are writable by uid `1000` (or root). |
| Customer pays but order stays at `pending` | Verify endpoint not hit | Check the redirect URL the gateway sent: it should land on `/payment/complete?tx_ref=...&gateway=...`. If not, the gateway config is missing a redirect URL. |
| `429 too many requests` on login | Rate limiter kicked in | Wait 60s or, in dev, restart the backend (in-memory bucket clears). |
| Frontend builds fail with "useSearchParams not in Suspense" | A new page added `useSearchParams()` without a `<Suspense>` boundary | Wrap the calling client component in `<Suspense>` (see `app/(public)/signin/page.tsx` for the pattern). |
| Coolify deploy succeeds but app is unhealthy | Healthcheck path wrong, or app didn't start | Backend health is `/api/health`. Frontend health is `/`. Check Logs for the actual startup error. |
| Emails not sending | SMTP not configured or credentials wrong | Check the backend logs for `mailer: SMTP not fully configured` or specific SMTP error lines. Invites stay copy-the-link in the admin UI when SMTP is missing. |
| Customer reports their session got logged out unexpectedly | JWT TTL is 24h; cookie expires at the same time | Working as designed. The cookie refresh-on-activity is a future improvement. |

## 18. Things explicitly NOT done

These are flagged for honesty — the system works without them, but
they're known gaps:

- **Help chat with RAG over site content** (Phase 5, deferred per
  user choice).
- **Refund flow in admin UI** — set status to cancelled, refund
  out-of-band with the gateway.
- **Per-customer admin password reset email** — only self-serve
  via profile or direct DB edit.
- **Horizontal scaling** — rate-limit map is in-memory; for >1
  instance you'd swap to Redis.
- **Webhook signature validation** for Flutterwave webhooks is
  permissive — relies on the secret hash header but doesn't HMAC-
  verify the payload bytes. Acceptable since we always re-verify
  via the API after seeing a webhook, but tighten if you care.
- **Image processing pipeline** — uploads get one WebP re-encode;
  no responsive variants, no CDN. Fine for current traffic.
- **PWA service worker** — manifest is there but no offline mode.

## 19. Where to ask

Everything's in the repos. If a future you is confused:

1. Read `backend/README.md` and `frontend/README.md` first — they
   cover build/run/deploy in their own terms.
2. This document for the system-level overview.
3. The commit messages are descriptive (intentional rule of the
   project) — `git log --oneline` gives a quick feature history.
4. Migrations are the source of truth for data shape; structs in
   `internal/store/*.go` mirror them.

The code is comment-rich — most "why is this here" questions have
an answer in the nearest comment.
