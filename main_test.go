package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"toss/internal/workspace"
)

func TestParseCreateRequestAroundNew(t *testing.T) {
	left, ok, err := parseCreateRequest([]string{"--lang", "flask", "new", "api"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("left-side flags did not parse as create request")
	}

	right, ok, err := parseCreateRequest([]string{"new", "api", "--lang", "flask"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("right-side flags did not parse as create request")
	}

	if left != right {
		t.Fatalf("requests differ: left=%+v right=%+v", left, right)
	}
}

func TestParseCreateRequestTTLAroundNew(t *testing.T) {
	left, ok, err := parseCreateRequest([]string{"--lang", "go", "--ttl", "3d", "new", "api"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("left-side TTL did not parse as create request")
	}

	right, ok, err := parseCreateRequest([]string{"new", "api", "--lang", "go", "--ttl", "3d"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("right-side TTL did not parse as create request")
	}

	if left != right {
		t.Fatalf("requests differ: left=%+v right=%+v", left, right)
	}
	if left.ttl != 72*time.Hour {
		t.Fatalf("ttl = %v, want 72h", left.ttl)
	}
}

func TestParseCreateRequestTemplateAliasAndNoVenv(t *testing.T) {
	got, ok, err := parseCreateRequest([]string{"new", "demo", "--template", "python", "--no-venv"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("request did not parse as create request")
	}
	if got.name != "demo" || got.template != "python" || !got.noVenv {
		t.Fatalf("unexpected request: %+v", got)
	}
}

func TestParseCreateRequestLeavesCleanFlagsAlone(t *testing.T) {
	_, ok, err := parseCreateRequest([]string{"clean", "--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("clean parsed as create request")
	}
}

func TestParseCreateRequestUnknownFlag(t *testing.T) {
	_, _, err := parseCreateRequest([]string{"new", "api", "--wat"})
	if err == nil {
		t.Fatal("unknown flag succeeded, want error")
	}
}

func TestParseCreateRequestInvalidTTL(t *testing.T) {
	for _, args := range [][]string{
		{"--ttl", "3"},
		{"new", "api", "--ttl", "days"},
		{"new", "api", "--ttl", "1w"},
		{"new", "api", "--ttl", "-3d"},
		{"new", "api", "--ttl", "0m"},
	} {
		if _, _, err := parseCreateRequest(args); err == nil {
			t.Fatalf("parseCreateRequest(%v) succeeded, want error", args)
		}
	}
}

func TestFormatExpires(t *testing.T) {
	now := time.Date(2026, 6, 26, 15, 30, 0, 0, time.UTC)
	if got := formatExpires(nil, now); got != "never" {
		t.Fatalf("formatExpires(nil) = %q, want never", got)
	}
	future := now.Add(48 * time.Hour)
	if got := formatExpires(&future, now); got != "in 2d" {
		t.Fatalf("formatExpires(future) = %q, want in 2d", got)
	}
	past := now.Add(-time.Minute)
	if got := formatExpires(&past, now); got != "expired" {
		t.Fatalf("formatExpires(past) = %q, want expired", got)
	}
}

func TestListWorkspacesShowsExpirationStates(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now()
	if _, err := workspace.CreateWithOptions(cfg, workspace.CreateOptions{Name: "never", Template: "blank", Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.CreateWithOptions(cfg, workspace.CreateOptions{Name: "future", Template: "go", TTL: 3 * 24 * time.Hour, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.CreateWithOptions(cfg, workspace.CreateOptions{Name: "expired", Template: "python", TTL: time.Hour, Now: now.Add(-2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := listWorkspaces(cfg, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"never", "in ", "expired"} {
		if !strings.Contains(got, want) {
			t.Fatalf("list output missing %q:\n%s", want, got)
		}
	}
}

func testConfig(t *testing.T) workspace.Config {
	t.Helper()
	root := t.TempDir()
	return workspace.Config{
		Home:          filepath.Join(root, ".toss"),
		WorkspacesDir: filepath.Join(root, ".toss", "workspaces"),
		ProjectsDir:   filepath.Join(root, "Projects"),
	}
}
