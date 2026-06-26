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
		path, err := workspace.Create(cfg, req.name, time.Now())
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
			return createRequest{}, false, errors.New("usage: toss new <name> [--lang template] [--no-venv]")
		}
		return createRequest{}, false, nil
	}
}

func isNonCreateCommand(arg string) bool {
	switch arg {
	case "ls", "keep", "clean", "path", "init", "help", "-h", "--help":
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

	fmt.Fprintf(stdout, "%-32s  %-19s  %-19s  %-8s  %s\n", "NAME", "CREATED", "MODIFIED", "SIZE", "PATH")
	for _, item := range items {
		fmt.Fprintf(
			stdout,
			"%-32s  %-19s  %-19s  %-8s  %s\n",
			item.Name,
			item.Created.Format("2006-01-02 15:04"),
			item.Modified.Format("2006-01-02 15:04"),
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: toss clean [--dry-run] [--older-than 3d|12h|30m] [--force]")
	}

	olderThan, err := workspace.ParseAge(*olderThanRaw)
	if err != nil {
		return err
	}

	candidates, err := workspace.CleanCandidates(cfg, time.Now(), olderThan, *force)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		fmt.Fprintln(stdout, "Nothing to clean.")
		return nil
	}

	fmt.Fprintf(stdout, "Workspaces to delete from %s:\n", cfg.WorkspacesDir)
	for _, candidate := range candidates {
		fmt.Fprintf(stdout, "  %s  modified %s  %s\n", candidate.Name, candidate.Modified.Format("2006-01-02 15:04"), candidate.Path)
	}

	if *dryRun {
		fmt.Fprintln(stdout, "Dry run only; nothing deleted.")
		return nil
	}

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

	for _, candidate := range candidates {
		if err := workspace.Delete(cfg, candidate.Path); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "Deleted %d workspace(s).\n", len(candidates))
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

var shellWrapper = strings.TrimLeft(`
toss() {
  local out
  case "$1" in
    ""|"new")
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
  toss [--lang template]       Create a scratch workspace and print its path
  toss new <name> [--lang template]
                               Create a named workspace and print its path
  toss ls                      List disposable workspaces
  toss keep <name>             Promote the active workspace to a project
  toss clean [options]         Delete old disposable workspaces
  toss path                    Print the active toss workspace path
  toss init                    Print the shell wrapper
  toss help                    Show this help

Clean options:
  --dry-run                    Show deletions without deleting
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
