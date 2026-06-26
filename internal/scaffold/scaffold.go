package scaffold

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Options struct {
	Template string
	Slug     string
	NoVenv   bool
}

var supportedTemplates = map[string]bool{
	"blank":  true,
	"python": true,
	"go":     true,
	"rust":   true,
	"flask":  true,
	"sqlite": true,
	"node":   true,
	"static": true,
	"shell":  true,
}

func Apply(dir string, opts Options) error {
	template := opts.Template
	if template == "" {
		template = "blank"
	}
	if err := ValidateTemplate(template); err != nil {
		return err
	}
	if err := validateRoot(dir); err != nil {
		return err
	}

	slug := SafeSlug(opts.Slug)
	switch template {
	case "blank":
		return nil
	case "python":
		if err := writePython(dir); err != nil {
			return err
		}
		return maybeCreateVenv(dir, opts.NoVenv)
	case "flask":
		if err := writeFlask(dir); err != nil {
			return err
		}
		return maybeCreateVenv(dir, opts.NoVenv)
	case "go":
		return writeGo(dir, slug)
	case "rust":
		return writeRust(dir, slug)
	case "sqlite":
		return writeSQLite(dir)
	case "node":
		return writeNode(dir, slug)
	case "static":
		return writeStatic(dir)
	case "shell":
		return writeShell(dir)
	default:
		return fmt.Errorf("unknown template %q", template)
	}
}

func ValidateTemplate(template string) error {
	if template == "" {
		template = "blank"
	}
	if !supportedTemplates[template] {
		return fmt.Errorf("unknown template %q", template)
	}
	return nil
}

func Templates() []string {
	return []string{"blank", "python", "go", "rust", "flask", "sqlite", "node", "static", "shell"}
}

func SafeSlug(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "scratch"
	}
	if slug[0] >= '0' && slug[0] <= '9' {
		slug = "x-" + slug
	}
	return slug
}

func writePython(dir string) error {
	return writeFiles(dir, map[string]fileSpec{
		"main.py": {
			content: pythonMain,
			mode:    0o644,
		},
		"README.md": {
			content: "# Python toss workspace\n\nRun:\n\n```sh\npython main.py\n```\n",
			mode:    0o644,
		},
		".gitignore": {
			content: pythonGitignore,
			mode:    0o644,
		},
	})
}

func writeFlask(dir string) error {
	return writeFiles(dir, map[string]fileSpec{
		"app.py": {
			content: flaskApp,
			mode:    0o644,
		},
		"requirements.txt": {
			content: "flask\n",
			mode:    0o644,
		},
		"README.md": {
			content: "# Flask toss workspace\n\nRun:\n\n```sh\npip install -r requirements.txt\npython app.py\n```\n",
			mode:    0o644,
		},
		".gitignore": {
			content: flaskGitignore,
			mode:    0o644,
		},
	})
}

func writeGo(dir, slug string) error {
	return writeFiles(dir, map[string]fileSpec{
		"go.mod": {
			content: fmt.Sprintf("module toss/%s\n\ngo 1.23\n", slug),
			mode:    0o644,
		},
		"main.go": {
			content: goMain,
			mode:    0o644,
		},
		"README.md": {
			content: "# Go toss workspace\n\nRun:\n\n```sh\ngo run .\n```\n",
			mode:    0o644,
		},
		".gitignore": {
			content: "bin/\ndist/\n*.test\n",
			mode:    0o644,
		},
	})
}

func writeRust(dir, slug string) error {
	return writeFiles(dir, map[string]fileSpec{
		"Cargo.toml": {
			content: fmt.Sprintf("[package]\nname = %q\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\n", slug),
			mode:    0o644,
		},
		"src/main.rs": {
			content: rustMain,
			mode:    0o644,
		},
		"README.md": {
			content: "# Rust toss workspace\n\nRun:\n\n```sh\ncargo run\n```\n",
			mode:    0o644,
		},
		".gitignore": {
			content: "target/\nCargo.lock\n",
			mode:    0o644,
		},
	})
}

func writeSQLite(dir string) error {
	return writeFiles(dir, map[string]fileSpec{
		"schema.sql": {
			content: sqliteSchema,
			mode:    0o644,
		},
		"seed.sql": {
			content: "INSERT INTO notes (body) VALUES ('hello from toss');\n",
			mode:    0o644,
		},
		"queries.sql": {
			content: "SELECT * FROM notes;\n",
			mode:    0o644,
		},
		"README.md": {
			content: "# SQLite toss workspace\n\n```sh\nsqlite3 app.db < schema.sql\nsqlite3 app.db < seed.sql\nsqlite3 app.db < queries.sql\n```\n",
			mode:    0o644,
		},
		".gitignore": {
			content: "*.db\n*.db-shm\n*.db-wal\n",
			mode:    0o644,
		},
	})
}

func writeNode(dir, slug string) error {
	return writeFiles(dir, map[string]fileSpec{
		"package.json": {
			content: fmt.Sprintf(nodePackage, slug),
			mode:    0o644,
		},
		"index.js": {
			content: "console.log(\"hello from toss\");\n",
			mode:    0o644,
		},
		"README.md": {
			content: "# Node toss workspace\n\nRun:\n\n```sh\nnode index.js\n```\n",
			mode:    0o644,
		},
		".gitignore": {
			content: "node_modules/\n.env\ndist/\n",
			mode:    0o644,
		},
	})
}

func writeStatic(dir string) error {
	return writeFiles(dir, map[string]fileSpec{
		"index.html": {
			content: staticHTML,
			mode:    0o644,
		},
		"style.css": {
			content: staticCSS,
			mode:    0o644,
		},
		"script.js": {
			content: "document.querySelector(\"#message\").textContent = \"hello from toss\";\n",
			mode:    0o644,
		},
		"README.md": {
			content: "# Static toss workspace\n\nOpen `index.html` in a browser.\n",
			mode:    0o644,
		},
	})
}

func writeShell(dir string) error {
	return writeFiles(dir, map[string]fileSpec{
		"script.sh": {
			content: shellScript,
			mode:    0o755,
		},
		"README.md": {
			content: "# Shell toss workspace\n\nRun:\n\n```sh\n./script.sh\n```\n",
			mode:    0o644,
		},
		".gitignore": {
			content: "*.log\n.env\n",
			mode:    0o644,
		},
	})
}

func maybeCreateVenv(dir string, noVenv bool) error {
	if noVenv {
		return nil
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return errors.New("python3 is required to create .venv; pass --no-venv to skip it")
	}
	cmd := exec.Command(python, "-m", "venv", ".venv")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("failed to create .venv: %w", err)
		}
		return fmt.Errorf("failed to create .venv: %w: %s", err, msg)
	}
	return nil
}

type fileSpec struct {
	content string
	mode    os.FileMode
}

func writeFiles(root string, files map[string]fileSpec) error {
	for rel, spec := range files {
		if err := writeFile(root, rel, spec.content, spec.mode); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(root, rel, content string, mode os.FileMode) error {
	path, err := safePath(root, rel)
	if err != nil {
		return err
	}
	if err := safeMkdirAll(root, filepath.Dir(rel)); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlink: %s", path)
		}
		return fmt.Errorf("refusing to overwrite existing file: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

func validateRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("workspace must be a real directory: %s", root)
	}
	return nil
}

func safeMkdirAll(root, relDir string) error {
	if relDir == "." {
		return nil
	}
	clean := filepath.Clean(relDir)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to create directory outside workspace: %s", relDir)
	}
	current := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if info, err := os.Lstat(current); err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to use unsafe directory path: %s", current)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Mkdir(current, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func safePath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("refusing absolute scaffold path: %s", rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing scaffold path outside workspace: %s", rel)
	}
	root = filepath.Clean(root)
	path := filepath.Join(root, clean)
	relToRoot, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing scaffold path outside workspace: %s", rel)
	}
	return path, nil
}

const pythonMain = `def main():
    print("hello from toss")


if __name__ == "__main__":
    main()
`

const pythonGitignore = `.venv/
__pycache__/
*.pyc
.env
`

const flaskApp = `from flask import Flask

app = Flask(__name__)


@app.get("/")
def index():
    return {"message": "hello from toss"}


if __name__ == "__main__":
    app.run(debug=True)
`

const flaskGitignore = `.venv/
__pycache__/
*.pyc
.env
instance/
`

const goMain = `package main

import "fmt"

func main() {
	fmt.Println("hello from toss")
}
`

const rustMain = `fn main() {
    println!("hello from toss");
}
`

const sqliteSchema = `CREATE TABLE notes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  body TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const nodePackage = `{
  "name": "%s",
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "start": "node index.js"
  }
}
`

const staticHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Toss Static Workspace</title>
    <link rel="stylesheet" href="style.css">
  </head>
  <body>
    <main>
      <h1 id="message">loading...</h1>
    </main>
    <script src="script.js"></script>
  </body>
</html>
`

const staticCSS = `body {
  margin: 0;
  font-family: system-ui, sans-serif;
  color: #1f2937;
  background: #f8fafc;
}

main {
  min-height: 100vh;
  display: grid;
  place-items: center;
}
`

const shellScript = `#!/usr/bin/env sh
set -eu

echo "hello from toss"
`
