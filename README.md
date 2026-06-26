# toss

`toss` is a small disposable workspace manager for developers. It creates temporary scratch directories for experiments, then lets you delete them later or promote one into a real project.

The Go executable is named `toss-bin`. A shell function named `toss` wraps it so new workspaces can become the current shell directory.

## Install

```sh
go build -o toss-bin .
```

Move `toss-bin` somewhere on your `PATH`, such as `~/bin` or `~/go/bin`.

Add the shell wrapper to bash or zsh:

```sh
eval "$(toss-bin init)"
```

Put that line in your shell config, such as `~/.zshrc` or `~/.bashrc`.

## Usage

```sh
toss
toss --lang python
toss new api-test
toss new api-test --lang flask
toss ls
toss path
toss keep my-real-project
toss clean --dry-run
toss clean --older-than 3d
```

Workspaces live under `~/.toss/workspaces` by default. Set `TOSS_HOME` to use a different base directory.

Promoted projects move to `~/Projects/<name>` by default. Set `TOSS_PROJECTS_DIR` to use a different project directory.

## Scaffolds

```sh
toss --lang python
toss new api --lang flask
toss new cli --lang go
toss new scratch-db --lang sqlite
```

`--template` is also supported as an alias for `--lang`.

Supported templates:

```txt
blank
python
go
rust
flask
sqlite
node
static
shell
```

Scaffolds are intentionally minimal and offline. `toss` creates starter files, but it does not install dependencies or contact the network. Python and Flask templates create `.venv` with `python3 -m venv .venv` by default; pass `--no-venv` to skip that.

Python:

```sh
toss new parser --lang python
source .venv/bin/activate
python main.py
toss keep parser
```

Flask:

```sh
toss new tiny-api --lang flask
source .venv/bin/activate
pip install -r requirements.txt
python app.py
toss keep tiny-api
```

Go:

```sh
toss new cli-demo --lang go
go run .
toss keep cli-demo
```

Rust:

```sh
toss new rust-demo --lang rust
cargo run
toss keep rust-demo
```

SQLite:

```sh
toss new notes-db --lang sqlite
sqlite3 app.db < schema.sql
sqlite3 app.db < seed.sql
sqlite3 app.db < queries.sql
toss keep notes-db
```

## Safety

`toss clean` only deletes marked workspace directories inside the configured workspaces directory. A deletion target must be a directory, must contain a `.toss` marker file, and must be under `TOSS_HOME/workspaces`.

By default, `toss clean` deletes workspaces older than 7 days and will not delete workspaces modified in the last 24 hours unless `--force` is passed.
