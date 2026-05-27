# Kuza Kizazi — Backend

Go HTTP API powering the Kuza Kizazi site (content, commerce, courses, memberships, payments). Pairs with the Next.js frontend at [kk-front](https://github.com/ianadamskelly/kk-front).

## Stack

- Go 1.25
- [chi](https://github.com/go-chi/chi) router
- PostgreSQL via [pgx/v5](https://github.com/jackc/pgx)
- JWT auth ([golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt))
- Local-disk uploads (image processing via [HugoSmits86/nativewebp](https://github.com/HugoSmits86/nativewebp))
- Payment gateways: Flutterwave, Sifalopay
- SMTP mailer (optional)

## Layout

```
main.go                     # entry point: load env, open DB, seed admin, serve
internal/
  api/        # HTTP handlers and router (chi)
  config/     # env-var configuration
  store/      # Postgres data access
  store/migrations/  # SQL migrations, applied on boot
uploads/      # gitignored; image/file uploads land here
```

## Prerequisites

- Go 1.25+
- PostgreSQL 14+ reachable via `DATABASE_URL`

## Local development

```bash
cp .env.example .env
# edit .env: at minimum set DATABASE_URL and JWT_SECRET
createdb kuzakizazi       # or use any existing Postgres database
go mod download
go run .
```

On boot the server runs all pending SQL migrations under `internal/store/migrations/` and seeds an admin user (`SEED_ADMIN_EMAIL` / `SEED_ADMIN_PASSWORD`) if one doesn't exist. It then listens on `:$PORT` (default `8080`).

### Environment variables

Core:

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port. |
| `DATABASE_URL` | local default | Postgres connection string. |
| `JWT_SECRET` | dev placeholder — **change in prod** | Signing secret for JWTs. Use a long random string. |
| `SEED_ADMIN_EMAIL` / `SEED_ADMIN_PASSWORD` | dev defaults — **change in prod** | Admin user created on first boot. |
| `UPLOAD_DIR` | `uploads` | Where **public** uploads (cover images, public assets) are written. Served verbatim under `/uploads/*`. Mount a persistent volume here in prod. |
| `PROTECTED_UPLOAD_DIR` | `protected_uploads` | Where **protected** uploads land (digital downloads, course resources, member library files, course-task attachments). Must be outside `UPLOAD_DIR`; reads go through signed token endpoints only. Mount a separate persistent volume in prod. |
| `CORS_ORIGIN` | `http://localhost:3000` | Allowed origin for the frontend. Cannot be `*` since the customer session cookie requires Allow-Credentials. |
| `COOKIE_DOMAIN` | `""` | Parent domain for the `kk_session` HttpOnly cookie. Set to `.kuzakizazi.com` in prod so the cookie spans the frontend + the api subdomain. Leave blank in dev. |
| `COOKIE_SECURE` | `false` | Forces the `Secure` flag on the session cookie. Set to `true` in prod (HTTPS) so the cookie is never sent over plaintext. |
| `PUBLIC_BASE_URL` | `http://localhost:3000` | Public **site** URL (frontend). Used in emails and payment redirects. |
| `API_PUBLIC_URL` | `http://localhost:8080` | Public **API** URL (this backend). Used to render absolute download links in order emails. |
| `PAYMENT_CURRENCY` | `KES` | Default checkout currency. |

Payments (optional — leave blank to disable):

| Variable | Purpose |
| --- | --- |
| `FLUTTERWAVE_PUBLIC_KEY`, `FLUTTERWAVE_SECRET_KEY`, `FLUTTERWAVE_SECRET_HASH` | Flutterwave credentials. |
| `FLUTTERWAVE_BASE_URL` | Override Flutterwave API base (defaults to live). |
| `SIFALOPAY_API_USER`, `SIFALOPAY_API_KEY` | Sifalopay credentials. |
| `SIFALOPAY_BASE_URL`, `SIFALOPAY_VERIFY_URL`, `SIFALOPAY_CHECKOUT_URL` | Override Sifalopay endpoints. |
| `KES_PER_USD` | FX rate for converting KES → USD when charging Sifalo (default `130`). |

SMTP (optional — leave blank and invitations stay copy-the-link in the admin UI):

| Variable | Notes |
| --- | --- |
| `SMTP_HOST` / `MAIL_HOST` | SMTP server hostname. |
| `SMTP_PORT` / `MAIL_PORT` | Defaults to `587`. |
| `SMTP_USER` / `MAIL_USERNAME` | |
| `SMTP_PASS` / `MAIL_PASSWORD` | |
| `SMTP_FROM` / `MAIL_FROM_ADDRESS` | From address. |
| `SMTP_TLS` | `true`/`false`. Inferred from port `465` or `MAIL_SCHEME=smtps` if unset. |

## Build

```bash
go build -trimpath -ldflags="-s -w" -o kkapi .
./kkapi
```

## Docker

```bash
docker build -t kk-api .
docker run --rm -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host:5432/kuzakizazi?sslmode=disable" \
  -e JWT_SECRET="$(openssl rand -hex 32)" \
  -e CORS_ORIGIN="https://kuzakizazi.com" \
  -v kk-uploads:/app/uploads \
  -v kk-protected-uploads:/app/protected_uploads \
  kk-api
```

The image runs as a non-root `app` user. Public uploads live at `/app/uploads`; protected payloads live at `/app/protected_uploads`. Mount both paths wherever files must survive redeploys.

## Deploy on Coolify

1. Provision a **PostgreSQL** resource in Coolify; copy its internal connection string.
2. Create a new **Application** from this repo, branch `main`, build pack **Dockerfile**.
3. Set environment variables (at minimum `DATABASE_URL`, `JWT_SECRET`, `CORS_ORIGIN`, `PUBLIC_BASE_URL`, and any payment/SMTP credentials you need). Use Coolify's secret type for the secrets.
4. Add two **persistent volumes**: `/app/uploads` → e.g. `kk-uploads`, and `/app/protected_uploads` → e.g. `kk-protected-uploads`.
5. Assign a domain (e.g. `api.kuzakizazi.com`); Traefik issues TLS.
6. Deploy. Check logs for `Kuza Kizazi API listening on …` and confirm the seed log line.

After first deploy, **rotate the seed admin password** (or unset `SEED_ADMIN_*` once a real admin exists).
