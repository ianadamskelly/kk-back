package api

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"kuzakizazi/internal/config"
	"kuzakizazi/internal/store"
)

// imageExt is the set of file extensions that legitimately live in
// the public /uploads dir (cover images for products, library entries,
// posts, etc.). Anything else stored under /uploads/ that's still
// referenced by a download / library / submission row is a relic
// from before the protected-uploads split and gets relocated.
var imageExt = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
	".svg":  true,
	".avif": true,
}

// MigrateLegacyProtectedFiles moves payloads still living under
// UploadDir (with a non-image extension and referenced by a
// product_downloads / library_resources / course_task_submissions
// row) into ProtectedUploadDir, then rewrites their URL prefix to
// "/files/". Idempotent — safe to run on every boot.
//
// Sequence per file (chosen so the DB is never inconsistent):
//
//	1. Copy old → new (no-op if new already exists).
//	2. Update DB rows from /uploads/X to /files/X.
//	3. Delete old (best effort).
//
// If the copy fails the DB stays pointing at the still-existing old
// file. If the DB update fails the new copy is removed.
func MigrateLegacyProtectedFiles(ctx context.Context, cfg config.Config, st *store.Store) {
	urls, err := st.ListLegacyProtectedFileURLs(ctx)
	if err != nil {
		log.Printf("legacy protected-file scan failed: %v", err)
		return
	}
	moved, skipped, failed := 0, 0, 0
	for _, u := range urls {
		ext := strings.ToLower(filepath.Ext(u))
		if imageExt[ext] {
			skipped++
			continue
		}
		name := strings.TrimPrefix(u, "/uploads/")
		if name == "" || strings.Contains(name, "..") {
			failed++
			continue
		}
		oldPath := filepath.Join(cfg.UploadDir, name)
		newPath := filepath.Join(cfg.ProtectedUploadDir, name)
		newURL := "/files/" + name

		// Case 1: file already at the new location — just retarget DB.
		if _, err := os.Stat(newPath); err == nil {
			if err := st.RetargetProtectedFileURL(ctx, u, newURL); err != nil {
				log.Printf("retarget %s -> %s failed: %v", u, newURL, err)
				failed++
				continue
			}
			// Old copy is redundant; clean it up.
			_ = os.Remove(oldPath)
			moved++
			continue
		}

		// Case 2: old file gone too — DB row is stale. Still rewrite
		// the URL so future code paths consistently use /files/.
		if _, err := os.Stat(oldPath); os.IsNotExist(err) {
			if err := st.RetargetProtectedFileURL(ctx, u, newURL); err != nil {
				log.Printf("retarget %s -> %s failed: %v", u, newURL, err)
				failed++
				continue
			}
			skipped++
			continue
		}

		// Case 3: ordinary copy → update → delete.
		if err := copyProtectedFile(oldPath, newPath); err != nil {
			log.Printf("copy %s -> %s failed: %v", oldPath, newPath, err)
			failed++
			continue
		}
		if err := st.RetargetProtectedFileURL(ctx, u, newURL); err != nil {
			log.Printf("retarget %s -> %s failed: %v", u, newURL, err)
			_ = os.Remove(newPath)
			failed++
			continue
		}
		_ = os.Remove(oldPath)
		log.Printf("legacy file migrated: %s -> %s", u, newURL)
		moved++
	}
	if moved > 0 || failed > 0 {
		log.Printf("legacy protected-file migration: moved=%d skipped=%d failed=%d", moved, skipped, failed)
	}
}

func copyProtectedFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}
