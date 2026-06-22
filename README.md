<div align="center">

<img src="web/logo.svg" width="112" height="112" alt="ChitHub logo" />

# ChitHub

**Manage all your git repositories at once.**

A fast, native git client for the folder full of repos you actually work in —
GitHub Desktop's everyday workflow, plus the multi-repo superpowers it never had.
One self-contained binary. No Electron.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)
![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)
[![CI](https://github.com/Amirhat/ChitHub/actions/workflows/test.yml/badge.svg)](https://github.com/Amirhat/ChitHub/actions/workflows/test.yml)

</div>

---

Most git clients open **one** repository. ChitHub opens a **folder of them** — a
microservices checkout, a pile of side projects, a “monorepo of repos” — and
shows the state of every one at a glance. Fetch, pull, push, stage and commit per
repo, in bulk, or by stepping through a guided review so nothing slips through.

It’s a single Go binary with an embedded web UI: no Node, no runtime, nothing to
install. Git operations shell out to your real `git`, so your existing
credentials just work.

## Features

#### At a glance
- A live dashboard of every repo in a collection — ahead / behind / diverged /
  dirty status, last commit, last fetch, with filters and search.
- **Auto-refresh:** the UI updates itself the moment anything changes on disk —
  a commit, a fetch, a branch switch, or an edit from another tool.

#### Commit & diff
- A genuinely **readable diff**: line numbers, syntax highlighting, word-level
  (intra-line) diff, and expandable context.
- Stage by file, by hunk, or by individual **line** — and discard by file or
  hunk — like `git add -p`, but you can see it.
- Commit, **amend**, undo the last commit; discards go to a recoverable stash.

#### History & branches
- Paginated **history with a commit graph**.
- **Revert**, **cherry-pick**, **tag**, and a **guided reset** (soft / mixed /
  hard) that explains each mode and can leave a safety branch behind first.
- Branch **switch / create / rename / delete**, **merge**, **rebase**, with a
  built-in **conflict-resolution UI**.

#### Sync
- **Fetch**, **pull** (fast-forward / rebase / merge), **push** (with
  `--force-with-lease` and a warning before pushing to `main`).
- **Sync** — fetch → pull → push in one click — **publish** a new branch, and
  **clone** from a URL.

#### Multi-repo superpowers
- **Review** — walk through every repo that needs a commit, push, or pull, one at
  a time.
- **Bulk commit / branch-switch / sync** across all selected repos.
- **Cross-repo search** (`git grep` everywhere), **run a command** in every repo,
  and **workspace snapshots** that save and restore which branch each repo is on.

#### Productivity
- A fuzzy **command palette** (`⌘K`), **light / dark** themes, and one-click
  **open in** browser, editor, terminal, or Finder.

## Install

### macOS — `.dmg`

Download `ChitHub-<version>.dmg` from the [latest release](https://github.com/Amirhat/ChitHub/releases),
drag **ChitHub** into Applications, and open it.

> The build is ad-hoc signed, so the **first** launch needs a right-click →
> **Open** (or run `xattr -dr com.apple.quarantine /Applications/ChitHub.app`).

### Homebrew — macOS & Linux

```bash
brew install Amirhat/tap/chithub
chithub
```

## Usage

Launch ChitHub and it opens in its own window. Point it at a **collection** — a
folder that contains your repositories — and it scans every repo inside.

- **Collections** — track several parent folders and switch between them from the
  picker under the title. Removing a collection only forgets it; nothing on disk
  is touched.
- **Commit** — click a repo to open its diff drawer, pick the files / hunks /
  lines to include, and commit. Unselected changes are left exactly as they were.
- **Review** — the `▶ Review` button steps through repos that need attention so
  you can clear them one by one (commits & pushes, or pulls).
- **Bulk** — select repos with the checkboxes (or none = all visible) and use the
  top-bar actions or the bottom bar to act on the whole selection at once.

### Keyboard shortcuts

| Shortcut | Action |
|----------|--------|
| `⌘K` / `Ctrl+K` | Command palette |
| `⌘↵` / `Ctrl+Enter` | Commit |
| `R` | Refresh / rescan |
| `Esc` | Close the open dialog or drawer |

## Configuration

Collections and preferences live in `~/.chithub.json` and are edited through the
**⚙ Settings** panel: theme, default pull mode, background-fetch interval, font
size, *warn before pushing to main*, and *discard → stash*.

Command-line flags override the saved config:

| Flag | Description |
|------|-------------|
| `-root <dir>` | Scan a specific folder |
| `-port <n>` | Listen on a different port (default `7171`) |
| `-no-open` | Don’t open a window on start |
| `-dev <dir>` | Serve the UI from disk instead of the embedded copy |
| `-version` | Print the version and exit |

## Building from source

ChitHub needs only Go 1.24+ and `git` on your `PATH`.

```bash
go build -o chithub .   # build a standalone binary
./run.sh                # build, scan the parent folder, and open the app
go run . -dev ./web     # develop with the UI served live from disk
```

The web UI is embedded with `go:embed`, so the resulting binary is fully
self-contained.

## Tests

A 29-case end-to-end suite drives the real HTTP API against throwaway git repos
(local bare repos stand in for remotes, so it runs offline) and asserts the git
state after every operation.

```bash
go test ./...
```

CI runs the suite on every push.

## Releasing

Releases are cut by [GoReleaser](https://goreleaser.com) from a tag — it builds
the macOS (universal) and Linux binaries, publishes a GitHub Release, updates the
Homebrew tap, and attaches the `.dmg`.

```bash
git tag v0.1.0 && git push origin v0.1.0
```

The `.dmg` ships ad-hoc signed (clean launch on Apple Silicon, one right-click on
first open). For a fully Gatekeeper-clean app, set a `MACOS_SIGN_IDENTITY`
(Developer ID) secret and add a `notarytool` step — `packaging/make-dmg.sh`
already uses the identity when present.

## License

[MIT](LICENSE) © ChitHub contributors
