package workspace

import (
	"encoding/json"
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
	for _, invalid := range []string{"3", "days", "1w", "0m"} {
		if _, err := ParseAge(invalid); err == nil {
			t.Fatalf("ParseAge(%q) succeeded, want error", invalid)
		}
	}
}

func TestCreateWorkspaceWithoutTTL(t *testing.T) {
	cfg := testConfig(t)
	now := time.Date(2026, 6, 26, 15, 30, 0, 0, time.UTC)
	created, err := CreateWithOptions(cfg, CreateOptions{Name: "scratch", Template: "blank", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	meta := readMarker(t, created)
	if meta.Name != "scratch" || meta.Template != "blank" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	if meta.ExpiresAt != nil {
		t.Fatalf("ExpiresAt = %v, want nil", meta.ExpiresAt)
	}
}

func TestCreateWorkspaceWithTTL(t *testing.T) {
	cfg := testConfig(t)
	now := time.Date(2026, 6, 26, 15, 30, 0, 0, time.UTC)
	created, err := CreateWithOptions(cfg, CreateOptions{Name: "api-test", Template: "flask", TTL: 72 * time.Hour, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	meta := readMarker(t, created)
	if meta.ExpiresAt == nil {
		t.Fatal("ExpiresAt is nil, want value")
	}
	if want := now.Add(72 * time.Hour); !meta.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", meta.ExpiresAt, want)
	}
}

func TestListHandlesLegacyMarker(t *testing.T) {
	cfg := testConfig(t)
	dir := filepath.Join(cfg.WorkspacesDir, "2026-06-26-1530-legacy-a8f2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, MarkerFile), []byte("toss workspace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := List(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("List returned %d items, want 1", len(items))
	}
	if items[0].Expires != nil {
		t.Fatalf("legacy Expires = %v, want nil", items[0].Expires)
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

func TestCleanCandidatesIncludesExpiredTTL(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now()
	created, err := CreateWithOptions(cfg, CreateOptions{Name: "short", Template: "blank", TTL: time.Hour, Now: now.Add(-2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	candidates, err := CleanCandidates(cfg, now, 7*24*time.Hour, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("CleanCandidates returned %d candidates, want 1", len(candidates))
	}
	if candidates[0].Path != created || candidates[0].Reason != "expired TTL" {
		t.Fatalf("unexpected candidate: %+v", candidates[0])
	}
}

func TestCleanExpiredOnlySkipsOldNonTTLWorkspace(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now()
	old, err := CreateWithOptions(cfg, CreateOptions{Name: "old", Template: "blank", Now: now.Add(-10 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	expired, err := CreateWithOptions(cfg, CreateOptions{Name: "expired", Template: "blank", TTL: time.Hour, Now: now.Add(-2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if old == expired {
		t.Fatal("test setup created duplicate paths")
	}

	candidates, err := CleanCandidates(cfg, now, 7*24*time.Hour, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("CleanCandidates returned %d candidates, want 1", len(candidates))
	}
	if candidates[0].Path != expired {
		t.Fatalf("candidate path = %q, want %q", candidates[0].Path, expired)
	}
}

func TestCleanDoesNotDeleteRecentlyModifiedExpiredTTLWithoutForce(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now()
	if _, err := CreateWithOptions(cfg, CreateOptions{Name: "recent", Template: "blank", TTL: time.Hour, Now: now.Add(-2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	candidates, err := CleanCandidates(cfg, now, 7*24*time.Hour, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("CleanCandidates returned %d candidates, want 0", len(candidates))
	}
}

func readMarker(t *testing.T, dir string) Metadata {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, MarkerFile))
	if err != nil {
		t.Fatal(err)
	}
	var meta Metadata
	if err := json.Unmarshal(body, &meta); err != nil {
		t.Fatal(err)
	}
	return meta
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
