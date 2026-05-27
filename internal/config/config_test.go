package config

import "testing"

func TestValidateRejectsProtectedDirectoryInsidePublicUploads(t *testing.T) {
	cfg := Config{UploadDir: "/tmp/public", ProtectedUploadDir: "/tmp/public/protected"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected nested protected directory to be rejected")
	}

	cfg.ProtectedUploadDir = "/tmp/public"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected identical upload directories to be rejected")
	}
}

func TestValidateAllowsSeparateProtectedDirectory(t *testing.T) {
	cfg := Config{UploadDir: "/tmp/public", ProtectedUploadDir: "/tmp/protected"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected separate directories to pass validation: %v", err)
	}
}
