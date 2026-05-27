package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds all runtime configuration, loaded from environment variables.
type Config struct {
	Port              string
	DatabaseURL       string
	JWTSecret         string
	SeedAdminEmail    string
	SeedAdminPassword string
	UploadDir         string
	// ProtectedUploadDir holds files that should NEVER be served by
	// the public file handler — digital download payloads, course
	// resources, member library files, course task attachments. Access is gated by
	// signed tokens minted by /api/files/{token}.
	ProtectedUploadDir string
	CORSOrigin         string
	// CookieDomain scopes the session cookie. In production, set to
	// the parent domain (e.g. ".kuzakizazi.com") so the cookie is
	// shared by the frontend + the api subdomain. Leave blank in
	// dev so the browser scopes it to localhost.
	CookieDomain string
	// CookieSecure forces the Secure flag on session cookies. Set to
	// false only for HTTP dev. Defaults true in production.
	CookieSecure bool

	PublicBaseURL   string // frontend, e.g. https://kuzakizazi.com
	APIPublicURL    string // backend, e.g. https://api.kuzakizazi.com — used in emails for download links
	PaymentCurrency string

	FlutterwavePublicKey  string
	FlutterwaveSecretKey  string
	FlutterwaveSecretHash string
	FlutterwaveBaseURL    string

	SifalopayAPIUser     string
	SifalopayAPIKey      string
	SifalopayBaseURL     string
	SifalopayVerifyURL   string
	SifalopayCheckoutURL string
	KESPerUSD            float64 // FX rate used to convert KES totals when charging Sifalo (USD-only).

	// SMTP — leave blank to disable email; invites stay available as
	// copy-the-link in the admin UI.
	SMTPHost string
	SMTPPort string
	SMTPUser string
	SMTPPass string
	SMTPFrom string
	SMTPTLS  bool
}

// Load reads configuration from the environment, applying sensible defaults.
func Load() Config {
	return Config{
		Port:               env("PORT", "8080"),
		DatabaseURL:        env("DATABASE_URL", "postgres://postgres@localhost:5432/kuzakizazi?sslmode=disable"),
		JWTSecret:          env("JWT_SECRET", "dev-only-secret-change-me"),
		SeedAdminEmail:     env("SEED_ADMIN_EMAIL", "admin@kuzakizazi.com"),
		SeedAdminPassword:  env("SEED_ADMIN_PASSWORD", "admin123"),
		UploadDir:          env("UPLOAD_DIR", "uploads"),
		ProtectedUploadDir: env("PROTECTED_UPLOAD_DIR", "protected_uploads"),
		CORSOrigin:         env("CORS_ORIGIN", "http://localhost:3000"),
		CookieDomain:       env("COOKIE_DOMAIN", ""),
		CookieSecure:       envBool("COOKIE_SECURE", false),

		PublicBaseURL:   env("PUBLIC_BASE_URL", "http://localhost:3000"),
		APIPublicURL:    env("API_PUBLIC_URL", "http://localhost:8080"),
		PaymentCurrency: env("PAYMENT_CURRENCY", "KES"),

		FlutterwavePublicKey:  os.Getenv("FLUTTERWAVE_PUBLIC_KEY"),
		FlutterwaveSecretKey:  os.Getenv("FLUTTERWAVE_SECRET_KEY"),
		FlutterwaveSecretHash: os.Getenv("FLUTTERWAVE_SECRET_HASH"),
		FlutterwaveBaseURL:    env("FLUTTERWAVE_BASE_URL", "https://api.flutterwave.com/v3"),

		SifalopayAPIUser:     os.Getenv("SIFALOPAY_API_USER"),
		SifalopayAPIKey:      os.Getenv("SIFALOPAY_API_KEY"),
		SifalopayBaseURL:     env("SIFALOPAY_BASE_URL", "https://api.sifalopay.com/gateway/"),
		SifalopayVerifyURL:   env("SIFALOPAY_VERIFY_URL", "https://api.sifalopay.com/gateway/verify.php"),
		SifalopayCheckoutURL: env("SIFALOPAY_CHECKOUT_URL", "https://pay.sifalo.com/checkout/"),
		KESPerUSD:            envFloat("KES_PER_USD", 130.0),

		// Accept both SMTP_* and the existing MAIL_* env naming so the
		// invite email works against the credentials already in .env.
		SMTPHost: firstEnv("SMTP_HOST", "MAIL_HOST"),
		SMTPPort: firstEnvOr("587", "SMTP_PORT", "MAIL_PORT"),
		SMTPUser: firstEnv("SMTP_USER", "MAIL_USERNAME"),
		SMTPPass: firstEnv("SMTP_PASS", "MAIL_PASSWORD"),
		SMTPFrom: firstEnv("SMTP_FROM", "MAIL_FROM_ADDRESS"),
		SMTPTLS: envBool("SMTP_TLS",
			strings.EqualFold(os.Getenv("MAIL_SCHEME"), "smtps") ||
				os.Getenv("MAIL_PORT") == "465" ||
				os.Getenv("SMTP_PORT") == "465"),
	}
}

// Validate rejects configurations that would let the public upload file
// server traverse into protected payloads.
func (c Config) Validate() error {
	publicDir, err := filepath.Abs(c.UploadDir)
	if err != nil {
		return fmt.Errorf("resolve UPLOAD_DIR: %w", err)
	}
	protectedDir, err := filepath.Abs(c.ProtectedUploadDir)
	if err != nil {
		return fmt.Errorf("resolve PROTECTED_UPLOAD_DIR: %w", err)
	}
	rel, err := filepath.Rel(publicDir, protectedDir)
	if err != nil {
		return fmt.Errorf("compare upload directories: %w", err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return fmt.Errorf("PROTECTED_UPLOAD_DIR must be outside UPLOAD_DIR")
	}
	return nil
}

// firstEnv returns the first non-empty value among the given env vars.
func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// firstEnvOr is like firstEnv but returns the fallback when all are unset.
func firstEnvOr(fallback string, keys ...string) string {
	if v := firstEnv(keys...); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return fallback
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
