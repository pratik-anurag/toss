package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
	case "ls", "keep", "clean", "path", "ttl", "init", "help", "-h", "--help":
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
	fmt.Fprintf(stdout, "%-24s  %-8s  %-16s  %-16s  %-10s  %-8s  %s\n", "NAME", "TEMPLATE", "CREATED", "MODIFIED", "EXPIRES", "SIZE", "PATH")
	for _, item := range items {
		fmt.Fprintf(
			stdout,
			"%-24s  %-8s  %-16s  %-16s  %-10s  %-8s  %s\n",
			item.Name,
			item.Template,
			item.Created.Format("2006-01-02 15:04"),
			item.Modified.Format("2006-01-02 15:04"),
			formatExpires(item.Expires, now),
			formatBytes(item.Size),
			item.Path,
		)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: toss clean [--dry-run] [--expired] [--yes] [--older-than 3d|12h|30m] [--force]")
	}

	olderThan, err := workspace.ParseAge(*olderThanRaw)
	if err != nil {
		return err
	}

	candidates, err := workspace.CleanCandidates(cfg, time.Now(), olderThan, *force, *expiredOnly)
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
    ""|"new"|"--lang"|"--template"|"--ttl"|"--no-venv")
      out="$(toss-bin "$@")" || return
      if [ -d "$out" ]; then
        cd "$out"
      else
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
