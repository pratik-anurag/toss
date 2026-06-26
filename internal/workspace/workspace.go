package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const MarkerFile = ".toss"

type Config struct {
	Home          string
	WorkspacesDir string
	ProjectsDir   string
}

type Info struct {
	Name     string
	Path     string
	Created  time.Time
	Modified time.Time
	Size     int64
}

func ConfigFromEnv() (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}

	tossHome := os.Getenv("TOSS_HOME")
	if tossHome == "" {
		tossHome = filepath.Join(homeDir, ".toss")
	}
	projectsDir := os.Getenv("TOSS_PROJECTS_DIR")
	if projectsDir == "" {
		projectsDir = filepath.Join(homeDir, "Projects")
	}

	tossHome, err = normalizePath(tossHome)
	if err != nil {
		return Config{}, err
	}
	projectsDir, err = normalizePath(projectsDir)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Home:          tossHome,
		WorkspacesDir: filepath.Join(tossHome, "workspaces"),
		ProjectsDir:   projectsDir,
	}, nil
}

func Create(cfg Config, name string, now time.Time) (string, error) {
	slug := Slug(name)
	if slug == "" {
		return "", errors.New("workspace name must contain at least one letter or number")
	}
	if err := os.MkdirAll(cfg.WorkspacesDir, 0o755); err != nil {
		return "", err
	}

	for attempts := 0; attempts < 10; attempts++ {
		dir := filepath.Join(cfg.WorkspacesDir, fmt.Sprintf("%s-%s-%s", now.Format("2006-01-02-1504"), slug, randomSuffix()))
		if err := os.Mkdir(dir, 0o755); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		marker := filepath.Join(dir, MarkerFile)
		if err := os.WriteFile(marker, []byte("toss workspace\n"), 0o644); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
		return dir, nil
	}

	return "", errors.New("could not create a unique workspace directory")
}

func List(cfg Config) ([]Info, error) {
	entries, err := os.ReadDir(cfg.WorkspacesDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []Info
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		path := filepath.Join(cfg.WorkspacesDir, entry.Name())
		if !hasMarker(path) {
			continue
		}
		info, err := inspect(path)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Created.Before(out[j].Created)
	})

	return out, nil
}

func Keep(cfg Config, name, cwd string) (string, error) {
	src, err := FindActive(cfg, cwd)
	if err != nil {
		return "", err
	}
	slug := Slug(name)
	if slug == "" {
		return "", errors.New("project name must contain at least one letter or number")
	}

	if err := os.MkdirAll(cfg.ProjectsDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(cfg.ProjectsDir, slug)
	if _, err := os.Lstat(dest); err == nil {
		return "", fmt.Errorf("destination already exists: %s", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := os.Remove(filepath.Join(src, MarkerFile)); err != nil {
		return "", err
	}
	if err := os.Rename(src, dest); err != nil {
		_ = os.WriteFile(filepath.Join(src, MarkerFile), []byte("toss workspace\n"), 0o644)
		return "", err
	}

	return dest, nil
}

func FindActive(cfg Config, cwd string) (string, error) {
	cwd, err := normalizePath(cwd)
	if err != nil {
		return "", err
	}
	workspacesDir, err := normalizePath(cfg.WorkspacesDir)
	if err != nil {
		return "", err
	}

	for {
		if isWithin(cwd, workspacesDir) && hasMarker(cwd) {
			return cwd, nil
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}

	return "", errors.New("not inside a toss workspace")
}

func CleanCandidates(cfg Config, now time.Time, olderThan time.Duration, force bool) ([]Info, error) {
	items, err := List(cfg)
	if err != nil {
		return nil, err
	}

	var candidates []Info
	for _, item := range items {
		if now.Sub(item.Modified) < olderThan {
			continue
		}
		if !force && now.Sub(item.Modified) < 24*time.Hour {
			continue
		}
		candidates = append(candidates, item)
	}

	return candidates, nil
}

func Delete(cfg Config, path string) error {
	path, err := normalizePath(path)
	if err != nil {
		return err
	}
	workspacesDir, err := normalizePath(cfg.WorkspacesDir)
	if err != nil {
		return err
	}
	if !isWithin(path, workspacesDir) || path == workspacesDir {
		return fmt.Errorf("refusing to delete outside workspaces directory: %s", path)
	}

	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to delete non-directory workspace: %s", path)
	}
	if !hasMarker(path) {
		return fmt.Errorf("refusing to delete unmarked directory: %s", path)
	}

	return os.RemoveAll(path)
}

func ParseAge(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("age cannot be empty")
	}
	if strings.HasSuffix(raw, "d") {
		daysRaw := strings.TrimSuffix(raw, "d")
		days, err := time.ParseDuration(daysRaw + "h")
		if err != nil {
			return 0, fmt.Errorf("invalid age %q", raw)
		}
		d := days * 24
		if d < 0 {
			return 0, errors.New("age must be positive")
		}
		return d, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid age %q", raw)
	}
	if d < 0 {
		return 0, errors.New("age must be positive")
	}
	return d, nil
}

var slugRe = regexp.MustCompile(`-+`)

func Slug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(slugRe.ReplaceAllString(b.String(), "-"), "-")
}

func inspect(path string) (Info, error) {
	var size int64
	var newest time.Time
	if err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	}); err != nil {
		return Info{}, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}
	if newest.IsZero() {
		newest = info.ModTime()
	}
	created := createdFromName(filepath.Base(path), info.ModTime())
	return Info{
		Name:     filepath.Base(path),
		Path:     path,
		Created:  created,
		Modified: newest,
		Size:     size,
	}, nil
}

func createdFromName(name string, fallback time.Time) time.Time {
	if len(name) < len("2006-01-02-1504") {
		return fallback
	}
	created, err := time.ParseInLocation("2006-01-02-1504", name[:len("2006-01-02-1504")], time.Local)
	if err != nil {
		return fallback
	}
	return created
}

func hasMarker(path string) bool {
	info, err := os.Lstat(filepath.Join(path, MarkerFile))
	return err == nil && info.Mode().IsRegular()
}

func randomSuffix() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%04x", time.Now().UnixNano()&0xffff)
	}
	return hex.EncodeToString(b[:])
}

func normalizePath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = abs
	}
	return filepath.Clean(path), nil
}

func isWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
