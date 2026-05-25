package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"

	"kuzakizazi/internal/api"
	"kuzakizazi/internal/config"
	"kuzakizazi/internal/store"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()

	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		log.Fatalf("could not create upload dir: %v", err)
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

	handler := api.NewRouter(cfg, st)
	addr := ":" + cfg.Port
	log.Printf("Kuza Kizazi API listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
