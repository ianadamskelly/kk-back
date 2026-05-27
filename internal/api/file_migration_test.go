package api

import (
	"os"
	"path/filepath"
	"testing"

	"kuzakizazi/internal/config"
)

func TestLegacyProtectedPathFindsNestedFileForReissue(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		UploadDir:          filepath.Join(root, "uploads"),
		ProtectedUploadDir: filepath.Join(root, "protected_uploads"),
	}
	oldPath := filepath.Join(cfg.UploadDir, "protected", "lesson.png")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, migrate, err := legacyProtectedPath(cfg, "/files/lesson.png")
	if err != nil {
		t.Fatal(err)
	}
	if !migrate || got != oldPath {
		t.Fatalf("expected legacy file to migrate from %q, got path=%q migrate=%v", oldPath, got, migrate)
	}
}

func TestLegacyProtectedPathAllowsConfiguredFileAndRejectsMissingReference(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		UploadDir:          filepath.Join(root, "uploads"),
		ProtectedUploadDir: filepath.Join(root, "protected_uploads"),
	}
	currentPath := filepath.Join(cfg.ProtectedUploadDir, "guide.pdf")
	if err := os.MkdirAll(filepath.Dir(currentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentPath, []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, migrate, err := legacyProtectedPath(cfg, "/files/guide.pdf"); err != nil || migrate {
		t.Fatalf("expected current protected file to remain in place; migrate=%v err=%v", migrate, err)
	}
	if _, _, err := legacyProtectedPath(cfg, "/files/missing.pdf"); err == nil {
		t.Fatal("expected missing protected reference to prevent startup")
	}
}

func TestProtectedMigrationAliasesPairLegacyNestedURLs(t *testing.T) {
	aliases := protectedMigrationAliases("/files/lesson.png")
	if len(aliases) != 2 || aliases[1] != "/uploads/protected/lesson.png" {
		t.Fatalf("expected nested uploads alias for files URL, got %#v", aliases)
	}
	aliases = protectedMigrationAliases("/uploads/protected/lesson.png")
	if len(aliases) != 2 || aliases[1] != "/files/lesson.png" {
		t.Fatalf("expected files alias for nested upload URL, got %#v", aliases)
	}
}
