# ChitHub 🐙

A tiny single-binary desktop-style app (Go + an embedded web UI) to manage all
the git repositories inside a folder — like a lightweight, multi-repo GitHub
Desktop. Built for monorepo-of-repos setups where you juggle many services at
once.

Scan a folder, see at a glance how far ahead/behind each repo is, and fetch,
pull, push, stage and commit — per repo, in bulk, or by stepping through a
guided **Review** flow.

Highlights: a readable diff with **syntax highlighting** and **word-level diff**,
line/hunk staging *and* discarding, **one-click Sync** (fetch→pull→push), full
**history with a commit graph** (revert / cherry-pick / reset / tag), branch
**merge/rebase** with a **conflict-resolution UI**, multi-repo superpowers
(**bulk commit**, **bulk branch switch**, **cross-repo search**, **run a command
everywhere**), a **command palette (⌘K)**, **light/dark themes**, **Persian/RTL**,
and a live UI that **auto-refreshes** on any git change.

## Run

```bash
./run.sh
```

Builds the binary, scans the parent folder, starts a local server on
<http://127.0.0.1:7171>, and opens it in an app window (Chrome/Edge `--app`
mode) or your default browser.

Other ways to run:

```bash
go run . -root /path/to/folder     # scan a specific folder
go run . -port 8080                # different port
go run . -no-open                  # don't open a browser
go run . -dev ./web                # serve the UI from disk (live-edit, no rebuild)
go build -o chithub .              # standalone binary you can move anywhere
```

## Install

ChitHub publishes a macOS app and a Homebrew formula on each release (see
[Releasing](#releasing)).

**Homebrew (macOS + Linux):**

```bash
brew install Amirhat/tap/chithub
chithub                             # run it
```

**macOS .dmg:** download `ChitHub-<version>.dmg` from the
[Releases](https://github.com/Amirhat/ChitHub/releases) page, drag ChitHub to
Applications, and open it. It is unsigned, so the first launch needs
right-click → **Open** (or `xattr -dr com.apple.quarantine /Applications/ChitHub.app`).

## Collections

A **collection** is a parent folder that holds project repos. Track several and
switch between them from the picker under the title (🗂️). Add a folder, switch
the active one, or remove it from the list — removing only forgets it, it never
deletes anything on disk. The list is saved in `~/.chithub.json`.

## What it shows

- **Status badges** per repo: `✓ up to date`, `↑ N to push`, `↓ N to pull`,
  `⇅ diverged`, `no upstream`, `detached HEAD`, and `● N changes` (dirty).
- Last commit message + relative time, and when the repo was last fetched.
- Header counters: total repos / behind / ahead / dirty.
- Filters (All / Needs attention / Behind / Ahead / Dirty) and name search.

## Operations

| Action | Notes |
|--------|-------|
| **Fetch** | `git fetch --all --prune` |
| **Pull** | fast-forward only (default, safe), `--rebase`, or merge |
| **Push** | normal, or force push (`--force-with-lease`, confirmed); warns before pushing to `main` |
| **Sync** | fetch → pull → push in one step (per repo or across selected repos) |
| **Publish** | push a no-upstream branch and set its tracking remote |
| **Commit** | full staging area with per-file, per-hunk *and* per-line selection — see below |
| **Discard** | per-line, per-hunk, per-file or all changes — optionally moved to a recoverable stash |
| **Amend** | fold the selected changes into the last commit and/or reword it |
| **Undo last commit** | soft reset `HEAD~1` (changes return to the working tree) |
| **Branch** | switch, create, **rename**, **delete** (local/remote), **merge**, **rebase** |
| **Conflicts** | resolution UI: use ours/theirs, edit, then continue / abort / skip |
| **History** | full paginated history + commit graph; **revert / cherry-pick / reset / tag / view** |
| **Tags** | list, create (lightweight or annotated), delete, push |
| **Stash / Pop** | `git stash push -u` / `git stash pop` |
| **Clone** | paste a URL (and optional folder name) |
| **Open** | the repo on the web, in your editor, in a terminal, or in Finder |

### Live updates

ChitHub watches the active collection and **refreshes the UI automatically** when
anything changes on disk (a commit, fetch, branch switch, or an external edit) —
via a Server-Sent-Events stream, so the manual Refresh button is now optional.
Turn on **background auto-fetch** in Settings to be notified when upstreams move.

### Staging area & partial commits

Click a repo to open the staging drawer. Each changed file has a checkbox; click
a file to expand its **diff** — rendered with line numbers, **syntax
highlighting** and **word-level (intra-line) diff**. Every **hunk and every line**
has its own toggle, so you can commit only part of a file (like `git add -p`), and
**↕ expand context** to see more surrounding lines. A selective commit rebuilds
the index to match exactly what you picked, so unselected changes stay untouched.
The **↩ discard** buttons throw away a single hunk (or file) — moved to a
recoverable stash by default (toggle in Settings).

### Review (one repo at a time)

The **▶ Review** button walks you through the repos that need attention, one by
one, so nothing slips through:

- **Review commits & pushes** — steps through every dirty/ahead repo; for each
  you choose what to stage, commit, and push, then advance to the next.
- **Review pulls** — steps through every behind repo, showing the incoming
  commits, and you decide to pull (ff / rebase / merge) or skip.

### Bulk operations (multi-repo superpowers)

Select repos with the checkboxes (or none = "all visible"). The top-bar
**Fetch / Pull▾ / Push▾** act on the selection, and a contextual bar at the bottom
adds the things GitHub Desktop can't do across many repos at once:

- **Bulk commit** — stage and commit every selected dirty repo with one message.
- **Bulk switch/create branch** — check out (or create) the same branch everywhere.
- **Sync selected** — fetch→pull→push each selected repo.
- **Run…** — run a shell command in every selected repo and see the results in a grid.
- **Cross-repo search** — `git grep` across every repo in the collection (⌘K → Search).
- **Workspace snapshots** — save which branch each repo is on, and restore it later.

### Command palette, settings & themes

Press **⌘K / Ctrl+K** for a fuzzy command palette (every action + jump to any repo
or its history). The **⚙ Settings** panel has a **light/dark** theme, a
**Persian / RTL** language option, default pull mode, background-fetch interval,
font size, "warn before pushing to main", and "discard → stash". Preferences are
saved in `~/.chithub.json`.

## How it works

- Git operations shell out to real `git`, so your existing credentials (macOS
  keychain, etc.) just work. `GIT_TERMINAL_PROMPT=0` makes a missing credential
  fail fast instead of hanging.
- Status is read in one pass via `git status --porcelain=v2 --branch`, scanned
  across all repos in parallel.
- The web UI is embedded with `go:embed`, so the executable is self-contained —
  no Node, nothing to install.

## Tests

A full end-to-end suite (26 tests across `e2e_test.go` + `features_test.go`)
drives the real HTTP API against throwaway git repos (local bare repos stand in
for remotes, so it runs offline) and asserts the git state after every
operation — status/scan, the diff endpoint, whole-file / hunk / **line-level**
commits and **line discard**, expand-context, the **no-newline** marker, amend,
undo, stash, discard (and discard→stash), branch switch + auto-stash,
**delete/rename**, **merge + conflict resolution** (ours/continue/abort),
**revert / cherry-pick / reset**, **tags**, **history graph**, **sync/publish**,
**bulk commit/checkout**, **cross-repo search**, **snapshots**, **settings**,
clone/fetch/pull/push, collections, review queues, and commit-view (incl. merge
commits). Run it with:

```bash
go test ./...
```

CI runs it on every push via `.github/workflows/test.yml`.

## Code signing & notarization

The released `.app`/`.dmg` is **ad-hoc signed** (`codesign --sign -`) during the
build. That's enough for it to run cleanly on Apple Silicon (no "app is damaged"
error), but because it isn't signed by an identity Apple trusts, the **first
launch still needs right-click → Open** once.

To ship a truly warning-free app you need an **Apple Developer account ($99/yr)**:

1. Create a **Developer ID Application** certificate in your account and install
   it in your login keychain.
2. In the release workflow, set `MACOS_SIGN_IDENTITY` to that identity name
   (e.g. `Developer ID Application: Your Name (TEAMID)`) — `make-dmg.sh` already
   uses it when present.
3. **Notarize** the `.dmg` after building (Apple scans it; then it opens with no
   prompt at all):

   ```bash
   xcrun notarytool submit dist/ChitHub-*.dmg \
     --apple-id you@example.com --team-id TEAMID --password APP_SPECIFIC_PW --wait
   xcrun stapler staple dist/ChitHub-*.dmg
   ```

   Store the Apple ID / team ID / app-specific password as GitHub secrets and add
   the two commands as a workflow step.

Short version: **ad-hoc (now) = no "damaged" error, one right-click on first open;
Developer ID + notarization ($99/yr) = zero prompts.**

## Releasing

Releases are cut by [GoReleaser](https://goreleaser.com) from a tag, via
`.github/workflows/release.yml` (runs on a macOS runner):

```bash
git tag v0.1.0
git push origin v0.1.0
```

The workflow builds macOS (universal) + Linux binaries, publishes a GitHub
Release with archives, updates the Homebrew tap formula, and attaches a
`ChitHub-<version>.dmg`.

**One-time setup on GitHub:**

1. Push this folder to [`github.com/Amirhat/ChitHub`](https://github.com/Amirhat/ChitHub)
   (`git push -u origin main`).
2. Create a second public repo `github.com/Amirhat/homebrew-tap` for the formula.
3. Add a repo secret **`HOMEBREW_TAP_TOKEN`** — a fine-grained PAT with
   contents-write access to `homebrew-tap`.
4. The default `GITHUB_TOKEN` covers everything else; the owner is read
   automatically from the repo.

> Note: the `.dmg` is unsigned. To ship a Gatekeeper-clean app later, add Apple
> Developer signing + notarization to the workflow.

## Files

```
main.go        server bootstrap, browser auto-open, embed / -dev disk serving
handlers.go    core JSON HTTP API (repos, collections, review, commit, diff, …)
handlers_ext.go  API for the new features (history, tags, conflicts, bulk, …)
git.go         repo scanning, status parsing, core git operations
gitext.go      branches, stash, discard, clone, diff, selective/hunk commit,
               amend, undo, commit-show, line discard, no-newline, expand-context
features.go    sync, publish, branch delete/rename/merge/rebase, history+graph,
               revert/cherry-pick/reset, blame, tags, conflicts, integrations,
               multi-repo bulk ops, search, snapshots
watch.go       Server-Sent-Events live-refresh watcher
config.go      ~/.chithub.json — collections, active folder, settings
web/           index.html · style.css · app.js  (embedded UI)
e2e_test.go · features_test.go   end-to-end test suite (26 tests)
.goreleaser.yaml · .github/workflows/release.yml · packaging/  (release tooling)
```
