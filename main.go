package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"

	"kuzakizazi/internal/api"
	"kuzakizazi/internal/config"
	"kuzakizazi/internal/store"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		log.Fatalf("could not create upload dir: %v", err)
	}
	if err := os.MkdirAll(cfg.ProtectedUploadDir, 0o755); err != nil {
		log.Fatalf("could not create protected upload dir: %v", err)
	}

	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("could not open database: %v", err)
	}
	defer st.Close()

	if err := st.Seed(ctx, cfg.SeedAdminEmail, cfg.SeedAdminPassword); err != nil {
		log.Fatalf("could not seed database: %v", err)
	}

	// Reissue legacy protected payload names and move bytes out of any
	// publicly served or historic nested upload location.
	if err := api.MigrateLegacyProtectedFiles(ctx, cfg, st); err != nil {
		log.Fatalf("could not migrate protected files: %v", err)
	}

	runBackgroundMaintenance(ctx, st)

	handler := api.NewRouter(cfg, st)
	addr := ":" + cfg.Port
	log.Printf("Kuza Kizazi API listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func runBackgroundMaintenance(ctx context.Context, st *store.Store) {
	run := func() {
		if n, err := st.PublishDueScheduledPosts(ctx); err != nil {
			log.Printf("scheduled post publish failed: %v", err)
		} else if n > 0 {
			log.Printf("published %d scheduled post(s)", n)
		}
		cancelled, reviewed, err := st.CleanupStaleUnconfirmedOrders(ctx)
		if err != nil {
			log.Printf("stale order cleanup failed: %v", err)
			return
		}
		if cancelled > 0 || reviewed > 0 {
			log.Printf("stale order cleanup: auto-cancelled=%d payment-review=%d", cancelled, reviewed)
		}
		if n, err := st.ExpireOverdueMemberships(ctx); err != nil {
			log.Printf("membership expiry failed: %v", err)
		} else if n > 0 {
			log.Printf("expired %d overdue membership(s)", n)
		}
	}

	run()
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				run()
			case <-ctx.Done():
				return
			}
		}
	}()
}
