# ChitHub — Roadmap & Review

A full review of ChitHub today, with ideas for new features and improvements,
grouped by area and tagged with a rough priority and effort.

Legend — **Priority:** ⭐⭐⭐ high value · ⭐⭐ nice · ⭐ later. **Effort:** S/M/L.

---

## ✅ Shipped (2026-06-21 feature pass)

A large batch of the backlog below is now implemented, tested (26 end-to-end Go
tests) and live-verified:

- **Diff / Changes:** rebuilt on a CSS grid (fixes the tall-context-row bug),
  **syntax highlighting**, **word-level (intra-line) diff**, **expand context**,
  per-line + per-hunk selection, **discard line/hunk**, **no-newline** handling.
- **Live updates:** Server-Sent-Events watcher — the UI auto-refreshes on any git
  change; no manual Refresh needed. Optional background auto-fetch.
- **Sync** (fetch→pull→push) per-repo and across selected repos; **Publish branch**.
- **Branches:** delete / rename / merge / rebase (+ a conflict resolution UI with
  ours/theirs/edit and continue/abort/skip).
- **History:** full paginated history with a **commit graph**, search, and per-commit
  **revert / cherry-pick / reset / tag / view diff**. **Tags** manager.
- **Guided reset:** a dedicated **soft / mixed / hard** reset dialog that spells out
  what each mode keeps or discards, warns before a destructive hard reset, and can
  create a **safety backup branch** at the current tip first (recoverable in one click).
- **Multi-repo:** **bulk commit** (one message), **bulk branch switch/create**,
  **cross-repo search**, **run a command in every repo**, **workspace snapshots**.
- **Integrations:** open on web / in editor / in terminal; CI status via `gh`;
  create a PR via `gh` (or a compare URL).
- **UX:** **light theme**, **command palette (⌘K)**, **settings panel**,
  **Persian / RTL** option, configurable font size.
- **Safety:** **discard → stash** (recoverable), **warn before pushing to main**.
- **Distribution:** in-app **update check**; Developer-ID signing + notarization
  wired in the dmg script (see README).

The tables below keep the original review; remaining/▶ items are future work.

---

## 0. Known limitations & tech debt (fix first)

| Item | Notes | Pri | Eff |
|------|-------|-----|-----|
| No filesystem watching | Status is a full rescan on load + a coarse 60s timer. Watch `.git` + worktrees (fsnotify) and refresh only affected repos. | ⭐⭐⭐ | M |
| "No newline at end of file" | The line-level patch builder drops `\ No newline…` markers; a file with no trailing newline can mis-stage. Handle the marker. | ⭐⭐⭐ | S |
| Single commit-message field | Split into **summary + description** (50/72), with a length hint — matches Git conventions and GitHub Desktop. | ⭐⭐ | S |
| `-root` always re-activates | Launching via `run.sh -root X` forces X active each time, so a switched collection doesn't persist across restarts. Only seed when empty. | ⭐⭐ | S |
| No conflict UI | Merge/rebase/pull conflicts surface only as raw git output. Needs a resolution view. | ⭐⭐⭐ | L |
| No frontend tests | Backend has a full e2e suite; the web UI has none. Add Playwright smoke tests. | ⭐⭐ | M |
| Large diffs | The diff endpoint returns the whole file diff; very large diffs aren't virtualized. | ⭐ | M |
| Submodules | Not detected or handled. | ⭐ | M |

---

## 1. Diff / Changes / Commit

| Idea | Pri | Eff |
|------|-----|-----|
| **Syntax highlighting** in diffs (embed a tiny highlighter) — biggest readability win | ⭐⭐⭐ | M |
| **Word-level (intra-line) diff** — highlight exactly what changed in a line | ⭐⭐⭐ | M |
| **Expand context** — load more lines around a hunk (unfold) | ⭐⭐ | M |
| **Discard selected lines/hunk** — the inverse of line-staging | ⭐⭐ | S |
| Summary + description commit fields, char counter, Markdown preview | ⭐⭐ | S |
| **Co-authors / trailers** UI (`Co-authored-by`) | ⭐ | S |
| Image diffs (before/after) and binary-file summaries | ⭐ | M |
| Soft-wrap toggle + in-diff search | ⭐ | S |
| Stage/commit keyboard shortcuts, "stage all in file" | ⭐⭐ | S |

## 2. History

| Idea | Pri | Eff |
|------|-----|-----|
| **Real History view** (more than last 15, paginated) + a commit graph | ⭐⭐⭐ | L |
| Search history (message / author / path); per-file history & **blame** | ⭐⭐ | M |
| **Revert** a commit; **cherry-pick**; **reset** to a commit (soft/mixed/hard) | ⭐⭐ | M |
| Compare two commits / branches | ⭐⭐ | M |
| **Tags**: create / list / push / delete | ⭐⭐ | S |
| Restore a single file from a past commit | ⭐ | S |

## 3. Branches

| Idea | Pri | Eff |
|------|-----|-----|
| **Delete / rename** branch (local + remote) | ⭐⭐⭐ | S |
| **Merge** another branch into current (+ conflict handling) | ⭐⭐⭐ | M |
| **Rebase** current onto another branch | ⭐⭐ | M |
| **Publish branch** as a first-class action for no-upstream branches | ⭐⭐⭐ | S |
| Compare branches (ahead/behind + diff) | ⭐⭐ | M |

## 4. Remote / sync

| Idea | Pri | Eff |
|------|-----|-----|
| **Sync** button = fetch → pull → push in one go (GitHub-Desktop style) | ⭐⭐⭐ | S |
| Background auto-fetch + native notification on new upstream commits | ⭐⭐ | M |
| **Create a Pull/Merge Request** from a branch (GitHub/GitLab API) | ⭐⭐ | L |
| Multiple remotes (fork workflows: origin + upstream) | ⭐ | M |
| Better credential guidance when auth fails | ⭐⭐ | S |
| Prune remote-tracking branches | ⭐ | S |

## 5. Multi-repo — ChitHub's superpower (beyond GitHub Desktop)

| Idea | Pri | Eff |
|------|-----|-----|
| **Bulk commit** the same message across selected repos | ⭐⭐⭐ | S |
| **Bulk branch switch / create** across selected repos | ⭐⭐⭐ | S |
| **Cross-repo search** (grep across every repo in a collection) | ⭐⭐ | M |
| **Workspace snapshots** — save/restore which branch each repo is on | ⭐⭐ | M |
| **Run a command in every repo** (generalized `pull-all.sh`) with a results grid | ⭐⭐ | M |
| Repo groups/tags inside a collection (filter & act on a subset) | ⭐⭐ | S |
| Aggregate dashboard: behind/ahead/dirty totals, last activity, CI status | ⭐⭐ | M |
| Warn when repos are on an unexpected branch | ⭐ | S |

## 6. Integrations

| Idea | Pri | Eff |
|------|-----|-----|
| **CI status** per branch (GitHub Actions / GitLab pipelines) | ⭐⭐ | M |
| Open repo on the web (origin → browser); open in editor / terminal | ⭐⭐⭐ | S |
| PR/MR list & status per repo | ⭐⭐ | L |
| External mergetool for conflicts | ⭐ | S |

## 7. UX / polish

| Idea | Pri | Eff |
|------|-----|-----|
| **Light theme** + toggle (currently dark only) | ⭐⭐ | M |
| **Command palette** (⌘K) + fuzzy actions; j/k navigation | ⭐⭐ | M |
| **Settings panel**: port, auto-fetch interval, default pull mode, theme, font size | ⭐⭐ | S |
| Persian / RTL option | ⭐⭐ | M |
| Persist window size/position (the .app) | ⭐⭐ | S |
| Native notifications for long ops / new commits | ⭐ | S |
| Activity log of operations; undo toasts (recover a discard from stash) | ⭐⭐ | M |
| First-run onboarding; nicer empty/error states | ⭐ | S |
| Drag-reorder collections; pin/favorite repos | ⭐ | S |

## 8. Safety

| Idea | Pri | Eff |
|------|-----|-----|
| **Discard → stash** instead of hard delete (recoverable trash) | ⭐⭐⭐ | S |
| Warn before committing/pushing directly to `main`/protected branches | ⭐⭐ | S |
| Dry-run preview for bulk operations | ⭐⭐ | S |

## 9. Distribution / ops

| Idea | Pri | Eff |
|------|-----|-----|
| **Developer ID signing + notarization** (warning-free launch) — documented in README | ⭐⭐⭐ | S |
| Auto-update for the .app (Sparkle-style) | ⭐⭐ | M |
| Linux desktop packaging (AppImage / .deb) + Windows app window | ⭐ | M |
| Release changelog automation | ⭐ | S |

---

## Suggested next milestone (high-value, low-effort first)

1. **Sync button** (fetch→pull→push) + **Publish branch** + **bulk branch switch** — the daily-driver multi-repo wins. *(S)*
2. **Branch delete/rename** + **tags** + **revert/reset** — close the GitHub-Desktop gap. *(S–M)*
3. **Discard → stash** (recoverable) + **commit summary/description** + **`\ No newline` fix**. *(S)*
4. **Filesystem watching** (auto-refresh) — removes the manual Refresh. *(M)*
5. **Syntax highlighting** + **word-level diff** — make the diff best-in-class. *(M)*
6. **Open on web / in editor / in terminal** — tiny, high-use. *(S)*
