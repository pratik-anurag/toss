package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlankScaffold(t *testing.T) {
	dir := t.TempDir()
	if err := Apply(dir, Options{Template: "blank", Slug: "scratch"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("blank scaffold created %d entries, want 0", len(entries))
	}
}

func TestPythonScaffold(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	dir := t.TempDir()
	if err := Apply(dir, Options{Template: "python", Slug: "parser"}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, dir, "main.py", `print("hello from toss")`)
	assertFile(t, dir, ".gitignore", ".venv/")
	assertDir(t, dir, ".venv")
}

func TestPythonScaffoldNoVenv(t *testing.T) {
	dir := t.TempDir()
	if err := Apply(dir, Options{Template: "python", Slug: "parser", NoVenv: true}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, dir, "main.py", `print("hello from toss")`)
	if _, err := os.Stat(filepath.Join(dir, ".venv")); !os.IsNotExist(err) {
		t.Fatalf(".venv exists or unexpected error: %v", err)
	}
}

func TestGoScaffold(t *testing.T) {
	dir := t.TempDir()
	if err := Apply(dir, Options{Template: "go", Slug: "CLI Tool!!"}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, dir, "go.mod", "module toss/cli-tool")
	assertFile(t, dir, "main.go", "hello from toss")
}

func TestRustScaffold(t *testing.T) {
	dir := t.TempDir()
	if err := Apply(dir, Options{Template: "rust", Slug: "99 Rust Demo"}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, dir, "Cargo.toml", `name = "x-99-rust-demo"`)
	assertFile(t, dir, "src/main.rs", "hello from toss")
}

func TestFlaskScaffold(t *testing.T) {
	dir := t.TempDir()
	if err := Apply(dir, Options{Template: "flask", Slug: "tiny-api", NoVenv: true}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, dir, "app.py", "from flask import Flask")
	assertFile(t, dir, "requirements.txt", "flask")
}

func TestSQLiteScaffold(t *testing.T) {
	dir := t.TempDir()
	if err := Apply(dir, Options{Template: "sqlite", Slug: "notes-db"}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, dir, "schema.sql", "CREATE TABLE notes")
	assertFile(t, dir, "README.md", "sqlite3 app.db < schema.sql")
}

func TestUnknownTemplate(t *testing.T) {
	if err := Apply(t.TempDir(), Options{Template: "rails"}); err == nil {
		t.Fatal("unknown template succeeded, want error")
	}
}

func TestScaffoldDoesNotWriteThroughWorkspaceSymlink(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Apply(link, Options{Template: "go", Slug: "demo"}); err == nil {
		t.Fatal("Apply through symlink root succeeded, want error")
	}
}

func TestScaffoldDoesNotWriteThroughNestedSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "src")); err != nil {
		t.Fatal(err)
	}
	if err := Apply(dir, Options{Template: "rust", Slug: "demo"}); err == nil {
		t.Fatal("Apply through nested symlink succeeded, want error")
	}
}

func TestSafeSlugForProjectFiles(t *testing.T) {
	got := SafeSlug("99 Hello_World/こんにちは")
	if got != "x-99-hello-world" {
		t.Fatalf("SafeSlug = %q, want x-99-hello-world", got)
	}
	if strings.ContainsAny(got, "_/. ") {
		t.Fatalf("SafeSlug contains unsafe characters: %q", got)
	}
}

func TestShellScriptExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := Apply(dir, Options{Template: "shell", Slug: "demo"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "script.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("script.sh mode = %v, want executable", info.Mode())
	}
}

func assertFile(t *testing.T, root, rel, contains string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), contains) {
		t.Fatalf("%s does not contain %q:\n%s", rel, contains, string(body))
	}
}

func assertDir(t *testing.T, root, rel string) {
	t.Helper()
	info, err := os.Stat(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", rel)
	}
}
