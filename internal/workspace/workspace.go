package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	Template string
	DirName  string
	Path     string
	Created  time.Time
	Modified time.Time
	Expires  *time.Time
	Pinned   bool
	Note     string
	Size     int64
}

type CreateOptions struct {
	Name     string
	Template string
	TTL      time.Duration
	Now      time.Time
}

type Metadata struct {
	Version   int        `json:"version"`
	Name      string     `json:"name"`
	Template  string     `json:"template"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	Pinned    bool       `json:"pinned"`
	Note      string     `json:"note"`
}

type CleanCandidate struct {
	Info
	Reason string
}

type MatchError struct {
	Query   string
	Matches []Info
}

func (e MatchError) Error() string {
	if len(e.Matches) == 0 {
		return fmt.Sprintf("no workspace matches %q", e.Query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "multiple workspaces match %q\n\n", e.Query)
	for i, item := range e.Matches {
		fmt.Fprintf(&b, "  %d  %-24s  %-8s  %s\n", i+1, item.Name, item.Template, item.Path)
	}
	fmt.Fprintf(&b, "\nRun one of:\n  toss cd 1\n  toss cd %s", e.Matches[0].Name)
	return b.String()
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
	return CreateWithOptions(cfg, CreateOptions{Name: name, Template: "blank", Now: now})
}

func CreateWithOptions(cfg Config, opts CreateOptions) (string, error) {
	if opts.Template == "" {
		opts.Template = "blank"
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	slug := Slug(opts.Name)
	if slug == "" {
		return "", errors.New("workspace name must contain at least one letter or number")
	}
	if err := os.MkdirAll(cfg.WorkspacesDir, 0o755); err != nil {
		return "", err
	}

	for attempts := 0; attempts < 10; attempts++ {
		dir := filepath.Join(cfg.WorkspacesDir, fmt.Sprintf("%s-%s-%s", opts.Now.Format("2006-01-02-1504"), slug, randomSuffix()))
		if err := os.Mkdir(dir, 0o755); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		if err := WriteMetadata(dir, Metadata{
			Version:   1,
			Name:      slug,
			Template:  opts.Template,
			CreatedAt: opts.Now,
			ExpiresAt: expiresAt(opts.Now, opts.TTL),
		}); err != nil {
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
		return out[i].Modified.After(out[j].Modified)
	})

	return out, nil
}

func Find(cfg Config, query string) (Info, error) {
	items, err := List(cfg)
	if err != nil {
		return Info{}, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return Info{}, errors.New("workspace query cannot be empty")
	}
	if query == "latest" {
		if len(items) == 0 {
			return Info{}, MatchError{Query: query}
		}
		return items[0], nil
	}
	if n, ok := parseIndex(query); ok {
		if n < 1 || n > len(items) {
			return Info{}, fmt.Errorf("workspace index %d is out of range", n)
		}
		return items[n-1], nil
	}

	var matches []Info
	needle := strings.ToLower(query)
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Name), needle) ||
			strings.Contains(strings.ToLower(item.DirName), needle) ||
			strings.Contains(strings.ToLower(item.Template), needle) ||
			strings.Contains(strings.ToLower(item.Note), needle) {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		return Info{}, MatchError{Query: query, Matches: matches}
	}
	return matches[0], nil
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

func CleanCandidates(cfg Config, now time.Time, olderThan time.Duration, force, expiredOnly, includePinned bool) ([]CleanCandidate, error) {
	items, err := List(cfg)
	if err != nil {
		return nil, err
	}

	var candidates []CleanCandidate
	for _, item := range items {
		if item.Pinned && !includePinned {
			continue
		}
		expired := item.Expires != nil && !now.Before(*item.Expires)
		old := now.Sub(item.Modified) >= olderThan
		if expiredOnly && !expired {
			continue
		}
		if !expiredOnly && !expired && !old {
			continue
		}
		if !force && now.Sub(item.Modified) < 24*time.Hour {
			continue
		}
		reason := fmt.Sprintf("older than %s", FormatDuration(olderThan))
		if expired {
			reason = "expired TTL"
		}
		candidates = append(candidates, CleanCandidate{Info: item, Reason: reason})
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
	marker := filepath.Join(path, MarkerFile)
	if !isWithin(marker, path) {
		return fmt.Errorf("refusing unsafe marker path: %s", marker)
	}

	return os.RemoveAll(path)
}

func Pin(path string, pinned bool) error {
	meta := MetadataForWorkspace(path)
	meta.Pinned = pinned
	return WriteMetadata(path, meta)
}

func SetNote(path, note string) error {
	if strings.ContainsAny(note, "\r\n") {
		return errors.New("note cannot contain newlines")
	}
	if len([]rune(note)) > 200 {
		return errors.New("note cannot be longer than 200 characters")
	}
	meta := MetadataForWorkspace(path)
	meta.Note = note
	return WriteMetadata(path, meta)
}

func Rename(cfg Config, item Info, newName string) (string, error) {
	slug := Slug(newName)
	if slug == "" {
		return "", errors.New("workspace name must contain at least one letter or number")
	}
	src, err := normalizePath(item.Path)
	if err != nil {
		return "", err
	}
	root, err := normalizePath(cfg.WorkspacesDir)
	if err != nil {
		return "", err
	}
	if !isWithin(src, root) || src == root {
		return "", fmt.Errorf("refusing to rename outside workspaces directory: %s", src)
	}
	if !hasMarker(src) {
		return "", fmt.Errorf("refusing to rename unmarked directory: %s", src)
	}
	destName := renamedDirName(filepath.Base(src), slug)
	dest := filepath.Join(root, destName)
	if !isWithin(dest, root) || dest == root {
		return "", fmt.Errorf("refusing unsafe rename destination: %s", dest)
	}
	if _, err := os.Lstat(dest); err == nil {
		return "", fmt.Errorf("destination already exists: %s", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	meta := MetadataForWorkspace(src)
	meta.Name = slug
	if err := WriteMetadata(src, meta); err != nil {
		return "", err
	}
	if err := os.Rename(src, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func WorkspaceSize(path string) int64 {
	var size int64
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode().IsRegular() {
			size += info.Size()
		}
		return nil
	})
	return size
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
		if d <= 0 {
			return 0, errors.New("age must be positive")
		}
		return d, nil
	}
	if !strings.HasSuffix(raw, "h") && !strings.HasSuffix(raw, "m") {
		return 0, fmt.Errorf("invalid age %q", raw)
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid age %q", raw)
	}
	if d <= 0 {
		return 0, errors.New("age must be positive")
	}
	return d, nil
}

func FormatDuration(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
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
	meta, metaOK := ReadMetadata(path)
	created := createdFromName(filepath.Base(path), info.ModTime())
	if metaOK && !meta.CreatedAt.IsZero() {
		created = meta.CreatedAt
	}
	if newest.IsZero() {
		newest = info.ModTime()
	}
	template := "blank"
	if metaOK && meta.Template != "" {
		template = meta.Template
	} else if !metaOK {
		template = "unknown"
	}
	name := filepath.Base(path)
	if metaOK && meta.Name != "" {
		name = meta.Name
	}
	return Info{
		Name:     name,
		Template: template,
		DirName:  filepath.Base(path),
		Path:     path,
		Created:  created,
		Modified: newest,
		Expires:  meta.ExpiresAt,
		Pinned:   meta.Pinned,
		Note:     meta.Note,
		Size:     size,
	}, nil
}

func ReadMetadata(path string) (Metadata, bool) {
	body, err := os.ReadFile(filepath.Join(path, MarkerFile))
	if err != nil {
		return Metadata{}, false
	}
	var meta Metadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return Metadata{}, false
	}
	if meta.Version == 0 {
		return Metadata{}, false
	}
	return meta, true
}

func MetadataForWorkspace(path string) Metadata {
	meta, ok := ReadMetadata(path)
	if ok {
		if meta.Name == "" {
			meta.Name = Slug(filepath.Base(path))
		}
		if meta.Template == "" {
			meta.Template = "unknown"
		}
		return meta
	}
	info, err := os.Stat(path)
	created := time.Now()
	if err == nil {
		created = createdFromName(filepath.Base(path), info.ModTime())
	}
	return Metadata{
		Version:   1,
		Name:      Slug(filepath.Base(path)),
		Template:  "unknown",
		CreatedAt: created,
		ExpiresAt: nil,
		Pinned:    false,
		Note:      "",
	}
}

func WriteMetadata(path string, meta Metadata) error {
	if meta.Version == 0 {
		meta.Version = 1
	}
	if meta.Template == "" {
		meta.Template = "blank"
	}
	body, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(filepath.Join(path, MarkerFile), body, 0o644)
}

func SetTTL(path string, now time.Time, ttl time.Duration) error {
	meta := MetadataForWorkspace(path)
	meta.ExpiresAt = expiresAt(now, ttl)
	return WriteMetadata(path, meta)
}

func ClearTTL(path string) error {
	meta := MetadataForWorkspace(path)
	meta.ExpiresAt = nil
	return WriteMetadata(path, meta)
}

func parseIndex(raw string) (int, bool) {
	var n int
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, raw != ""
}

func renamedDirName(oldName, slug string) string {
	parts := strings.Split(oldName, "-")
	if len(parts) >= 6 && len(parts[0]) == 4 && len(parts[1]) == 2 && len(parts[2]) == 2 && len(parts[3]) == 4 {
		prefix := strings.Join(parts[:4], "-")
		suffix := parts[len(parts)-1]
		return prefix + "-" + slug + "-" + suffix
	}
	return slug
}

func expiresAt(now time.Time, ttl time.Duration) *time.Time {
	if ttl <= 0 {
		return nil
	}
	expires := now.Add(ttl)
	return &expires
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
