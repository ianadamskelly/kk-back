package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kuzakizazi/internal/config"
	"kuzakizazi/internal/store"
)

// MigrateLegacyProtectedFiles reissues sensitive files that were once
// addressable through the public uploads tree. It covers old /uploads/
// references and /files/ references whose bytes were written under the
// historic uploads/protected directory. New /files/ records already stored
// under the configured protected root are left untouched.
func MigrateLegacyProtectedFiles(ctx context.Context, cfg config.Config, st *store.Store) error {
	urls, err := st.ListProtectedFileURLs(ctx)
	if err != nil {
		return fmt.Errorf("scan protected file references: %w", err)
	}
	moved := 0
	retargeted := make(map[string]bool)
	for _, storedURL := range urls {
		if retargeted[storedURL] {
			continue
		}
		oldPath, needsMigration, err := legacyProtectedPath(cfg, storedURL)
		if err != nil {
			return err
		}
		if !needsMigration {
			continue
		}
		if _, err := os.Stat(oldPath); err != nil {
			return fmt.Errorf("protected asset %s is referenced but unavailable at %s: %w", storedURL, oldPath, err)
		}
		newName, err := reissuedFileName(filepath.Ext(oldPath))
		if err != nil {
			return fmt.Errorf("generate replacement name for %s: %w", storedURL, err)
		}
		newPath := filepath.Join(cfg.ProtectedUploadDir, newName)
		newURL := "/files/" + newName
		if err := copyProtectedFile(oldPath, newPath); err != nil {
			return fmt.Errorf("copy protected asset %s: %w", storedURL, err)
		}
		if err := st.RetargetProtectedFileURL(ctx, storedURL, newURL); err != nil {
			_ = os.Remove(newPath)
			return fmt.Errorf("retarget protected asset %s: %w", storedURL, err)
		}
		for _, alias := range protectedMigrationAliases(storedURL) {
			retargeted[alias] = true
		}
		if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove exposed protected asset %s: %w", oldPath, err)
		}
		log.Printf("protected asset reissued: %s -> %s", storedURL, newURL)
		moved++
	}
	if moved > 0 {
		log.Printf("protected-file migration: reissued=%d", moved)
	}
	return nil
}

func protectedMigrationAliases(storedURL string) []string {
	switch {
	case strings.HasPrefix(storedURL, "/files/"):
		return []string{storedURL, "/uploads/protected/" + strings.TrimPrefix(storedURL, "/files/")}
	case strings.HasPrefix(storedURL, "/uploads/protected/"):
		return []string{storedURL, "/files/" + strings.TrimPrefix(storedURL, "/uploads/protected/")}
	default:
		return []string{storedURL}
	}
}

func legacyProtectedPath(cfg config.Config, storedURL string) (string, bool, error) {
	switch {
	case strings.HasPrefix(storedURL, "/uploads/"):
		name := strings.TrimPrefix(storedURL, "/uploads/")
		if invalidRelativeFileName(name) {
			return "", false, fmt.Errorf("invalid legacy upload path %q", storedURL)
		}
		return filepath.Join(cfg.UploadDir, name), true, nil
	case strings.HasPrefix(storedURL, "/files/"):
		name := strings.TrimPrefix(storedURL, "/files/")
		if invalidRelativeFileName(name) {
			return "", false, fmt.Errorf("invalid protected file path %q", storedURL)
		}
		oldPath := filepath.Join(cfg.UploadDir, "protected", name)
		if _, err := os.Stat(oldPath); err == nil {
			return oldPath, true, nil
		} else if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("inspect legacy protected asset %s: %w", oldPath, err)
		}
		newPath := filepath.Join(cfg.ProtectedUploadDir, name)
		if _, err := os.Stat(newPath); err == nil {
			return "", false, nil
		} else if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("inspect protected asset %s: %w", newPath, err)
		}
		return "", false, fmt.Errorf("protected asset %s is referenced but unavailable in legacy or configured storage", storedURL)
	default:
		return "", false, nil
	}
}

func invalidRelativeFileName(name string) bool {
	return name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, "\x00") || filepath.IsAbs(name)
}

func reissuedFileName(ext string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(buf) + strings.ToLower(ext), nil
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
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
