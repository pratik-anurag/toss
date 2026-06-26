package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSlug(t *testing.T) {
	tests := map[string]string{
		"api-test":     "api-test",
		" API Test!! ": "api-test",
		"demo__two":    "demo-two",
		"こんにちは":        "こんにちは",
		"---":          "",
	}
	for in, want := range tests {
		if got := Slug(in); got != want {
			t.Fatalf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseAge(t *testing.T) {
	tests := map[string]time.Duration{
		"3d":  72 * time.Hour,
		"12h": 12 * time.Hour,
		"30m": 30 * time.Minute,
	}
	for in, want := range tests {
		got, err := ParseAge(in)
		if err != nil {
			t.Fatalf("ParseAge(%q) unexpected error: %v", in, err)
		}
		if got != want {
			t.Fatalf("ParseAge(%q) = %v, want %v", in, got, want)
		}
	}

	if _, err := ParseAge("-1d"); err == nil {
		t.Fatal("ParseAge(-1d) succeeded, want error")
	}
}

func TestFindActiveFromNestedDirectory(t *testing.T) {
	cfg := testConfig(t)
	created, err := Create(cfg, "api-test", time.Date(2026, 6, 26, 15, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(created, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FindActive(cfg, nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != created {
		t.Fatalf("FindActive = %q, want %q", got, created)
	}
}

func TestDeleteRequiresMarkedWorkspaceInsideRoot(t *testing.T) {
	cfg := testConfig(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, MarkerFile), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Delete(cfg, outside); err == nil {
		t.Fatal("Delete outside root succeeded, want error")
	}

	unmarked := filepath.Join(cfg.WorkspacesDir, "unmarked")
	if err := os.MkdirAll(unmarked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Delete(cfg, unmarked); err == nil {
		t.Fatal("Delete unmarked workspace succeeded, want error")
	}
}

func TestKeepPromotesAndRemovesMarker(t *testing.T) {
	cfg := testConfig(t)
	created, err := Create(cfg, "scratch", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	dest, err := Keep(cfg, "Real Project", created)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cfg.ProjectsDir, "real-project")
	if dest != want {
		t.Fatalf("Keep = %q, want %q", dest, want)
	}
	if _, err := os.Stat(filepath.Join(dest, MarkerFile)); !os.IsNotExist(err) {
		t.Fatalf("marker still exists or unexpected error: %v", err)
	}
}

func testConfig(t *testing.T) Config {
	t.Helper()
	root := t.TempDir()
	return Config{
		Home:          filepath.Join(root, ".toss"),
		WorkspacesDir: filepath.Join(root, ".toss", "workspaces"),
		ProjectsDir:   filepath.Join(root, "Projects"),
	}
}
