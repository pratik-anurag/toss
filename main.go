package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"toss/internal/scaffold"
	"toss/internal/workspace"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "toss: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	cfg, err := workspace.ConfigFromEnv()
	if err != nil {
		return err
	}

	if req, ok, err := parseCreateRequest(args); err != nil {
		return err
	} else if ok {
		if err := scaffold.ValidateTemplate(req.template); err != nil {
			return err
		}
		now := time.Now()
		path, err := workspace.CreateWithOptions(cfg, workspace.CreateOptions{
			Name:     req.name,
			Template: req.template,
			TTL:      req.ttl,
			Now:      now,
		})
		if err != nil {
			return err
		}
		if req.template != "blank" {
			fmt.Fprintf(stderr, "Applying %s template...\n", req.template)
		}
		if err := scaffold.Apply(path, scaffold.Options{
			Template: req.template,
			Slug:     req.name,
			NoVenv:   req.noVenv,
		}); err != nil {
			_ = os.RemoveAll(path)
			return err
		}
		fmt.Fprintln(stdout, path)
		return nil
	}

	switch args[0] {
	case "ls":
		if len(args) != 1 {
			return errors.New("usage: toss ls")
		}
		return listWorkspaces(cfg, stdout)
	case "keep":
		if len(args) != 2 {
			return errors.New("usage: toss keep <name>")
		}
		path, err := workspace.Keep(cfg, args[1], mustGetwd())
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, path)
	case "clean":
		return cleanWorkspaces(cfg, args[1:], stdout, stderr, stdin)
	case "cd":
		if len(args) != 2 {
			return errors.New("usage: toss cd <query>")
		}
		item, err := workspace.Find(cfg, args[1])
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, item.Path)
	case "rm":
		return rmWorkspace(cfg, args[1:], stdout, stderr, stdin)
	case "pin":
		return pinWorkspace(cfg, args[1:], stdout, true)
	case "unpin":
		return pinWorkspace(cfg, args[1:], stdout, false)
	case "note":
		return noteWorkspace(cfg, args[1:], stdout)
	case "rename":
		return renameWorkspace(cfg, args[1:], stdout)
	case "size":
		return sizeWorkspaces(cfg, args[1:], stdout, stderr)
	case "doctor":
		if len(args) != 1 {
			return errors.New("usage: toss doctor")
		}
		return doctor(cfg, stdout)
	case "path":
		if len(args) != 1 {
			return errors.New("usage: toss path")
		}
		path, err := workspace.FindActive(cfg, mustGetwd())
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, path)
	case "ttl":
		return ttlWorkspace(cfg, args[1:], stdout)
	case "init":
		if len(args) != 1 {
			return errors.New("usage: toss init")
		}
		fmt.Fprint(stdout, shellWrapper)
	case "help", "-h", "--help":
		if len(args) != 1 {
			return errors.New("usage: toss help")
		}
		fmt.Fprint(stdout, helpText)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], strings.TrimSpace(helpText))
	}

	return nil
}

type createRequest struct {
	name     string
	template string
	noVenv   bool
	ttl      time.Duration
}

func parseCreateRequest(args []string) (createRequest, bool, error) {
	req := createRequest{name: "scratch", template: "blank"}
	if len(args) == 0 {
		return req, true, nil
	}
	if isNonCreateCommand(args[0]) {
		return createRequest{}, false, nil
	}

	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--lang", "--template":
			i++
			if i >= len(args) {
				return createRequest{}, false, fmt.Errorf("%s requires a template name; try toss help", args[i-1])
			}
			req.template = args[i]
		case "--no-venv":
			req.noVenv = true
		case "--ttl":
			i++
			if i >= len(args) {
				return createRequest{}, false, fmt.Errorf("%s requires a duration; try toss help", args[i-1])
			}
			ttl, err := workspace.ParseAge(args[i])
			if err != nil {
				return createRequest{}, false, fmt.Errorf("invalid TTL %q: %w", args[i], err)
			}
			req.ttl = ttl
		default:
			if strings.HasPrefix(args[i], "-") {
				return createRequest{}, false, fmt.Errorf("unknown flag %q; try toss help", args[i])
			}
			remaining = append(remaining, args[i])
		}
	}

	switch len(remaining) {
	case 0:
		return req, true, nil
	case 2:
		if remaining[0] != "new" {
			return createRequest{}, false, nil
		}
		req.name = remaining[1]
		return req, true, nil
	default:
		if remaining[0] == "new" {
			return createRequest{}, false, errors.New("usage: toss new <name> [--lang template] [--ttl duration] [--no-venv]")
		}
		return createRequest{}, false, nil
	}
}

func isNonCreateCommand(arg string) bool {
	switch arg {
	case "ls", "keep", "clean", "cd", "rm", "pin", "unpin", "note", "rename", "size", "doctor", "path", "ttl", "init", "help", "-h", "--help":
		return true
	default:
		return false
	}
}

func listWorkspaces(cfg workspace.Config, stdout io.Writer) error {
	items, err := workspace.List(cfg)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		fmt.Fprintln(stdout, "No disposable workspaces.")
		return nil
	}

	now := time.Now()
	hasNote := false
	for _, item := range items {
		if item.Note != "" {
			hasNote = true
			break
		}
	}
	if hasNote {
		fmt.Fprintf(stdout, "%-3s  %-24s  %-8s  %-3s  %-10s  %-16s  %-8s  %-28s  %s\n", "#", "NAME", "TEMPLATE", "PIN", "EXPIRES", "MODIFIED", "SIZE", "NOTE", "PATH")
	} else {
		fmt.Fprintf(stdout, "%-3s  %-24s  %-8s  %-3s  %-10s  %-16s  %-8s  %s\n", "#", "NAME", "TEMPLATE", "PIN", "EXPIRES", "MODIFIED", "SIZE", "PATH")
	}
	for _, item := range items {
		pin := "no"
		if item.Pinned {
			pin = "yes"
		}
		if hasNote {
			fmt.Fprintf(stdout, "%-3d  %-24s  %-8s  %-3s  %-10s  %-16s  %-8s  %-28s  %s\n", rowNumber(items, item), item.Name, item.Template, pin, formatExpires(item.Expires, now), item.Modified.Format("2006-01-02 15:04"), formatBinaryBytes(item.Size), truncate(item.Note, 28), item.Path)
		} else {
			fmt.Fprintf(stdout, "%-3d  %-24s  %-8s  %-3s  %-10s  %-16s  %-8s  %s\n", rowNumber(items, item), item.Name, item.Template, pin, formatExpires(item.Expires, now), item.Modified.Format("2006-01-02 15:04"), formatBinaryBytes(item.Size), item.Path)
		}
	}

	return nil
}

func cleanWorkspaces(cfg workspace.Config, args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "show what would be deleted")
	olderThanRaw := fs.String("older-than", "7d", "delete workspaces older than this duration")
	force := fs.Bool("force", false, "allow deletion of workspaces modified in the last 24 hours")
	expiredOnly := fs.Bool("expired", false, "only delete TTL-expired workspaces")
	yes := fs.Bool("yes", false, "skip confirmation")
	includePinned := fs.Bool("include-pinned", false, "include pinned workspaces")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: toss clean [--dry-run] [--expired] [--yes] [--include-pinned] [--older-than 3d|12h|30m] [--force]")
	}

	olderThan, err := workspace.ParseAge(*olderThanRaw)
	if err != nil {
		return err
	}

	candidates, err := workspace.CleanCandidates(cfg, time.Now(), olderThan, *force, *expiredOnly, *includePinned)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		fmt.Fprintln(stdout, "Nothing to clean.")
		return nil
	}

	fmt.Fprintln(stdout, "The following workspaces will be deleted:")
	fmt.Fprintln(stdout)
	for _, candidate := range candidates {
		fmt.Fprintf(stdout, "  %-24s  %-13s  %s\n", candidate.Name, candidate.Reason, candidate.Path)
	}

	if *dryRun {
		fmt.Fprintln(stdout, "Dry run only; nothing deleted.")
		return nil
	}

	if !*yes {
		fmt.Fprint(stdout, "Delete these workspaces? [y/N] ")
		answer, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(stdout, "Aborted.")
			return nil
		}
	}

	for _, candidate := range candidates {
		if err := workspace.Delete(cfg, candidate.Path); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "Deleted %d workspace(s).\n", len(candidates))
	return nil
}

func rmWorkspace(cfg workspace.Config, args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	var query string
	var yes, force bool
	for _, arg := range args {
		switch arg {
		case "--yes":
			yes = true
		case "--force":
			force = true
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("unknown flag %q", arg)
			}
			if query != "" {
				return errors.New("usage: toss rm <query> [--yes] [--force]")
			}
			query = arg
		}
	}
	if query == "" {
		return errors.New("usage: toss rm <query> [--yes] [--force]")
	}
	item, err := workspace.Find(cfg, query)
	if err != nil {
		return err
	}
	if item.Pinned && !force {
		return fmt.Errorf("workspace %q is pinned; use --force to delete it", item.Name)
	}
	fmt.Fprintln(stdout, "Delete workspace?")
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "  %s  %s\n", item.Name, item.Path)
	fmt.Fprintln(stdout)
	if !yes {
		fmt.Fprint(stdout, "This cannot be undone. [y/N] ")
		answer, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(stdout, "Aborted.")
			return nil
		}
	}
	if err := workspace.Delete(cfg, item.Path); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Deleted %s\n", item.Name)
	return nil
}

func pinWorkspace(cfg workspace.Config, args []string, stdout io.Writer, pinned bool) error {
	if len(args) > 1 {
		if pinned {
			return errors.New("usage: toss pin [query]")
		}
		return errors.New("usage: toss unpin [query]")
	}
	item, err := currentOrQuery(cfg, args)
	if err != nil {
		return err
	}
	if err := workspace.Pin(item.Path, pinned); err != nil {
		return err
	}
	if pinned {
		fmt.Fprintf(stdout, "Pinned %s\n", item.Name)
	} else {
		fmt.Fprintf(stdout, "Unpinned %s\n", item.Name)
	}
	return nil
}

func noteWorkspace(cfg workspace.Config, args []string, stdout io.Writer) error {
	if len(args) > 2 {
		return errors.New("usage: toss note [query] [note|clear]")
	}
	if len(args) == 0 {
		item, err := currentOrQuery(cfg, nil)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, item.Note)
		return nil
	}
	if len(args) == 2 {
		item, err := workspace.Find(cfg, args[0])
		if err != nil {
			return err
		}
		note := args[1]
		if note == "clear" {
			note = ""
		}
		if err := workspace.SetNote(item.Path, note); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Updated note for %s\n", item.Name)
		return nil
	}
	if args[0] == "clear" {
		item, err := currentOrQuery(cfg, nil)
		if err != nil {
			return err
		}
		if err := workspace.SetNote(item.Path, ""); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Cleared note for %s\n", item.Name)
		return nil
	}
	if item, err := workspace.Find(cfg, args[0]); err == nil {
		fmt.Fprintln(stdout, item.Note)
		return nil
	}
	item, err := currentOrQuery(cfg, nil)
	if err != nil {
		return err
	}
	if err := workspace.SetNote(item.Path, args[0]); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Updated note for %s\n", item.Name)
	return nil
}

func renameWorkspace(cfg workspace.Config, args []string, stdout io.Writer) error {
	if len(args) != 1 && len(args) != 2 {
		return errors.New("usage: toss rename <name> OR toss rename <query> <name>")
	}
	var item workspace.Info
	var newName string
	var err error
	if len(args) == 1 {
		item, err = currentOrQuery(cfg, nil)
		newName = args[0]
	} else {
		item, err = workspace.Find(cfg, args[0])
		newName = args[1]
	}
	if err != nil {
		return err
	}
	inside := false
	if cwd, err := os.Getwd(); err == nil {
		rel, relErr := filepath.Rel(item.Path, cwd)
		inside = relErr == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
	}
	newPath, err := workspace.Rename(cfg, item, newName)
	if err != nil {
		return err
	}
	if inside {
		fmt.Fprintln(stdout, newPath)
	} else {
		fmt.Fprintf(stdout, "Renamed %s to %s\n", item.Name, workspace.Slug(newName))
	}
	return nil
}

func sizeWorkspaces(cfg workspace.Config, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("size", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sortBy := fs.String("sort", "size", "sort by size or modified")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || (*sortBy != "size" && *sortBy != "modified") {
		return errors.New("usage: toss size [--sort size|modified]")
	}
	items, err := workspace.List(cfg)
	if err != nil {
		return err
	}
	var total int64
	for i := range items {
		items[i].Size = workspace.WorkspaceSize(items[i].Path)
		total += items[i].Size
	}
	sort.Slice(items, func(i, j int) bool {
		if *sortBy == "modified" {
			return items[i].Modified.After(items[j].Modified)
		}
		return items[i].Size > items[j].Size
	})
	fmt.Fprintf(stdout, "Total: %s\n\n", formatBinaryBytes(total))
	fmt.Fprintf(stdout, "%-24s  %-8s  %-8s  %s\n", "NAME", "TEMPLATE", "SIZE", "PATH")
	for _, item := range items {
		fmt.Fprintf(stdout, "%-24s  %-8s  %-8s  %s\n", item.Name, item.Template, formatBinaryBytes(item.Size), item.Path)
	}
	return nil
}

func doctor(cfg workspace.Config, stdout io.Writer) error {
	items, err := workspace.List(cfg)
	if err != nil {
		return err
	}
	var pinned, expired int
	var total int64
	now := time.Now()
	for _, item := range items {
		if item.Pinned {
			pinned++
		}
		if item.Expires != nil && !now.Before(*item.Expires) {
			expired++
		}
		total += workspace.WorkspaceSize(item.Path)
	}
	fmt.Fprintln(stdout, "toss doctor")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Binary")
	if bin, err := exec.LookPath("toss-bin"); err == nil {
		fmt.Fprintf(stdout, "  toss-bin: %s\n", bin)
	} else {
		fmt.Fprintln(stdout, "  toss-bin: not found on PATH")
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Config")
	fmt.Fprintf(stdout, "  TOSS_HOME: %s\n", envStatus("TOSS_HOME"))
	fmt.Fprintf(stdout, "  home: %s\n", cfg.Home)
	fmt.Fprintf(stdout, "  workspaces: %s\n", cfg.WorkspacesDir)
	fmt.Fprintf(stdout, "  TOSS_PROJECTS_DIR: %s\n", envStatus("TOSS_PROJECTS_DIR"))
	fmt.Fprintf(stdout, "  projects: %s\n", cfg.ProjectsDir)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Health")
	fmt.Fprintf(stdout, "  workspaces dir: %s\n", dirHealth(cfg.WorkspacesDir, true))
	fmt.Fprintf(stdout, "  projects dir: %s\n", dirHealth(cfg.ProjectsDir, false))
	fmt.Fprintf(stdout, "  workspaces: %d\n", len(items))
	fmt.Fprintf(stdout, "  pinned: %d\n", pinned)
	fmt.Fprintf(stdout, "  expired: %d\n", expired)
	fmt.Fprintf(stdout, "  total size: %s\n", formatBinaryBytes(total))
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Tools")
	for _, tool := range []string{"python3", "go", "cargo", "node", "sqlite3", "git"} {
		status := "missing"
		if _, err := exec.LookPath(tool); err == nil {
			status = "found"
		}
		fmt.Fprintf(stdout, "  %s: %s\n", tool, status)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Shell")
	fmt.Fprintln(stdout, "  Add this to your shell config if `toss` does not cd:")
	fmt.Fprintln(stdout, "    eval \"$(toss-bin init)\"")
	return nil
}

func ttlWorkspace(cfg workspace.Config, args []string, stdout io.Writer) error {
	if len(args) > 1 {
		return errors.New("usage: toss ttl [duration|clear]")
	}
	path, err := workspace.FindActive(cfg, mustGetwd())
	if err != nil {
		return err
	}
	now := time.Now()
	if len(args) == 0 {
		meta := workspace.MetadataForWorkspace(path)
		if meta.ExpiresAt == nil {
			fmt.Fprintln(stdout, "never")
			return nil
		}
		fmt.Fprintln(stdout, formatExpires(meta.ExpiresAt, now))
		return nil
	}
	if args[0] == "clear" {
		if err := workspace.ClearTTL(path); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "never")
		return nil
	}
	ttl, err := workspace.ParseAge(args[0])
	if err != nil {
		return fmt.Errorf("invalid TTL %q: %w", args[0], err)
	}
	if err := workspace.SetTTL(path, now, ttl); err != nil {
		return err
	}
	meta := workspace.MetadataForWorkspace(path)
	fmt.Fprintln(stdout, formatExpires(meta.ExpiresAt, now))
	return nil
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "toss: %v\n", err)
		os.Exit(1)
	}
	return wd
}

func currentOrQuery(cfg workspace.Config, args []string) (workspace.Info, error) {
	if len(args) == 1 {
		return workspace.Find(cfg, args[0])
	}
	path, err := workspace.FindActive(cfg, mustGetwd())
	if err != nil {
		return workspace.Info{}, err
	}
	items, err := workspace.List(cfg)
	if err != nil {
		return workspace.Info{}, err
	}
	for _, item := range items {
		if item.Path == path {
			return item, nil
		}
	}
	return workspace.Info{Path: path, Name: workspace.MetadataForWorkspace(path).Name}, nil
}

func rowNumber(items []workspace.Info, item workspace.Info) int {
	for i := range items {
		if items[i].Path == item.Path {
			return i + 1
		}
	}
	return 0
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func formatBinaryBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	value := float64(size)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f%s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1fPiB", value/unit)
}

func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "..."
}

func envStatus(name string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return "not set"
}

func dirHealth(path string, mustExist bool) string {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if mustExist {
			return "missing"
		}
		return "creatable"
	}
	if err != nil {
		return "error"
	}
	if !info.IsDir() {
		return "not a directory"
	}
	return "ok"
}

func formatExpires(expires *time.Time, now time.Time) string {
	if expires == nil {
		return "never"
	}
	if !now.Before(*expires) {
		return "expired"
	}
	remaining := expires.Sub(now).Round(time.Minute)
	if remaining < time.Minute {
		return "in <1m"
	}
	if remaining >= 48*time.Hour && remaining%(24*time.Hour) == 0 {
		return fmt.Sprintf("in %dd", int(remaining/(24*time.Hour)))
	}
	if remaining >= 24*time.Hour {
		return fmt.Sprintf("in %dd", int(remaining/(24*time.Hour)))
	}
	if remaining >= time.Hour {
		return fmt.Sprintf("in %dh", int(remaining/time.Hour))
	}
	return fmt.Sprintf("in %dm", int(remaining/time.Minute))
}

var shellWrapper = strings.TrimLeft(`
toss() {
  local out
  case "$1" in
    ""|"new"|"cd"|"rename"|"--lang"|"--template"|"--ttl"|"--no-venv")
      out="$(toss-bin "$@")" || return
      if [ -d "$out" ]; then
        cd "$out"
      elif [ -n "$out" ]; then
        printf "%s\n" "$out"
      fi
      ;;
    *)
      toss-bin "$@"
      ;;
  esac
}
`, "\n")

var helpText = strings.TrimLeft(`
toss is a disposable workspace manager.

Usage:
  toss [--lang template] [--ttl duration]
                               Create a scratch workspace and print its path
  toss new <name> [--lang template] [--ttl duration]
                               Create a named workspace and print its path
  toss ls                      List disposable workspaces
  toss cd <query>              Print a workspace path for shell cd
  toss rm <query>              Delete one disposable workspace
  toss pin [query]             Protect a workspace from cleanup
  toss unpin [query]           Remove pinned protection
  toss note [query] [note]     Inspect or set a workspace note
  toss rename <name>           Rename the active disposable workspace
  toss size                    Show workspace disk usage
  toss doctor                  Debug installation and environment
  toss keep <name>             Promote the active workspace to a project
  toss clean [options]         Delete old disposable workspaces
  toss path                    Print the active toss workspace path
  toss ttl [duration|clear]    Inspect or change the active workspace TTL
  toss init                    Print the shell wrapper
  toss help                    Show this help

Clean options:
  --dry-run                    Show deletions without deleting
  --expired                    Only consider TTL-expired workspaces
  --yes                        Skip confirmation
  --include-pinned             Include pinned workspaces
  --older-than 3d|12h|30m      Default: 7d
  --force                      Allow deleting workspaces modified in the last 24h

Examples:
  eval "$(toss-bin init)"
  toss
  toss --lang python
  toss new api-test --lang flask
  toss new cli-tool --lang go
  toss keep my-real-project
  toss clean --older-than 3d
  toss ls

Navigation:
  toss cd latest
  toss cd api
  toss cd 3

Management:
  toss rm api-test
  toss rm api-test --yes
  toss pin
  toss unpin
  toss note "trying sqlite WAL behavior"
  toss rename auth-spike
  toss size
  toss doctor

TTL examples:
  toss --ttl 3d
  toss new api-test --lang flask --ttl 12h
  toss ttl 2d
  toss ttl clear
  toss clean --expired
  toss clean --expired --yes

Templates:
  blank
  python
  go
  rust
  flask
  sqlite
  node
  static
  shell

Environment:
  TOSS_HOME                    Default: ~/.toss
  TOSS_PROJECTS_DIR            Default: ~/Projects
`, "\n")
