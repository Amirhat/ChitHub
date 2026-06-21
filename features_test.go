package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Additional end-to-end coverage for the features added on top of the original
// suite: sync/publish, branch delete/rename, merge + conflict resolution,
// revert/cherry-pick/reset, tags, history graph, line discard, expand context,
// no-newline handling, bulk operations, search, snapshot and settings.

func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	write(t, dir, name, content)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", msg)
}

func bareRemote(t *testing.T, base, name string) string {
	t.Helper()
	bare := filepath.Join(base, name)
	git(t, base, "init", "-q", "--bare", "-b", "main", bare)
	return bare
}

func TestPublishAndSync(t *testing.T) {
	root := t.TempDir()
	remotes := t.TempDir()
	bare := bareRemote(t, remotes, "origin.git")

	work := initRepo(t, root, "work")
	git(t, work, "remote", "add", "origin", bare)
	srv := newServer(t, root)

	// publish: pushes the branch and sets upstream
	mustOK(t, op(t, srv, "/api/repo/work/publish", nil))
	info := repoFromList(t, srv, "work")
	if !info["hasUpstream"].(bool) {
		t.Fatal("publish did not set an upstream")
	}

	// new local commit -> sync should push it
	commitFile(t, work, "n.txt", "n\n", "n")
	r := op(t, srv, "/api/repo/work/sync", map[string]any{"mode": "ff"})
	mustOK(t, r)
	if !strings.Contains(git(t, remotes, "--git-dir", bare, "log", "--oneline"), "n") {
		t.Error("sync did not push the local commit to origin")
	}
}

func TestBranchDeleteRename(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	git(t, dir, "branch", "topic")
	srv := newServer(t, root)

	// rename topic -> feature
	mustOK(t, op(t, srv, "/api/repo/r/branch-rename", map[string]any{"from": "topic", "to": "feature"}))
	if !strings.Contains(git(t, dir, "branch", "--list"), "feature") {
		t.Error("rename did not produce 'feature'")
	}
	// delete feature
	mustOK(t, op(t, srv, "/api/repo/r/branch-delete", map[string]any{"branch": "feature", "force": true}))
	if strings.Contains(git(t, dir, "branch", "--list"), "feature") {
		t.Error("delete did not remove 'feature'")
	}
}

func TestMergeConflictResolve(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	commitFile(t, dir, "f.txt", "base\n", "base")
	git(t, dir, "checkout", "-q", "-b", "dev")
	commitFile(t, dir, "f.txt", "dev\n", "dev change")
	git(t, dir, "checkout", "-q", "main")
	commitFile(t, dir, "f.txt", "main\n", "main change")

	srv := newServer(t, root)

	// merge dev -> conflict (OK=false expected)
	if op(t, srv, "/api/repo/r/merge", map[string]any{"branch": "dev"}).OK {
		t.Fatal("expected a merge conflict")
	}
	cs := getObj(t, srv, "/api/repo/r/conflicts")
	if cs["inProgress"] != "merge" {
		t.Fatalf("inProgress = %v, want merge", cs["inProgress"])
	}
	if len(toStrSlice(cs["files"])) != 1 {
		t.Fatalf("want 1 conflicted file, got %v", cs["files"])
	}

	// resolve using ours (main), then continue
	mustOK(t, op(t, srv, "/api/repo/r/resolve", map[string]any{"path": "f.txt", "side": "ours"}))
	mustOK(t, op(t, srv, "/api/repo/r/sequencer", map[string]any{"op": "merge", "action": "continue"}))
	if strings.TrimSpace(git(t, dir, "show", "HEAD:f.txt")) != "main" {
		t.Error("resolve-ours + continue did not keep our content")
	}
	if cs2 := getObj(t, srv, "/api/repo/r/conflicts"); cs2["inProgress"] != "" {
		t.Error("merge still marked in progress after continue")
	}
}

func TestMergeAbort(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	commitFile(t, dir, "f.txt", "base\n", "base")
	git(t, dir, "checkout", "-q", "-b", "dev")
	commitFile(t, dir, "f.txt", "dev\n", "dev")
	git(t, dir, "checkout", "-q", "main")
	commitFile(t, dir, "f.txt", "main\n", "main")

	srv := newServer(t, root)
	op(t, srv, "/api/repo/r/merge", map[string]any{"branch": "dev"})
	mustOK(t, op(t, srv, "/api/repo/r/sequencer", map[string]any{"op": "merge", "action": "abort"}))
	if strings.TrimSpace(git(t, dir, "show", "HEAD:f.txt")) != "main" {
		t.Error("abort did not restore main content")
	}
}

func TestRevertCherryPickReset(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	commitFile(t, dir, "f.txt", "one\n", "one")
	commitFile(t, dir, "f.txt", "one\ntwo\n", "two")
	top := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	prev := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD~1"))

	srv := newServer(t, root)

	// revert the "two" commit -> file back to "one"
	mustOK(t, op(t, srv, "/api/repo/r/revert", map[string]any{"hash": top}))
	if strings.TrimSpace(git(t, dir, "show", "HEAD:f.txt")) != "one" {
		t.Error("revert did not undo the change")
	}

	// reset --hard back to prev
	mustOK(t, op(t, srv, "/api/repo/r/reset", map[string]any{"hash": prev, "mode": "hard"}))
	if strings.TrimSpace(git(t, dir, "rev-parse", "HEAD")) != prev {
		t.Error("reset --hard did not move HEAD")
	}

	// cherry-pick the original "two" commit on top
	mustOK(t, op(t, srv, "/api/repo/r/cherry-pick", map[string]any{"hash": top}))
	if strings.TrimSpace(git(t, dir, "show", "HEAD:f.txt")) != "one\ntwo" {
		t.Error("cherry-pick did not re-apply the change")
	}
}

func TestTags(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	srv := newServer(t, root)

	mustOK(t, op(t, srv, "/api/repo/r/tag", map[string]any{"action": "create", "name": "v1.0.0", "message": "first"}))
	tags := getObj(t, srv, "/api/repo/r/tags")["tags"].([]any)
	if len(tags) != 1 || tags[0].(map[string]any)["name"] != "v1.0.0" {
		t.Fatalf("tags = %v", tags)
	}
	if !strings.Contains(git(t, dir, "tag", "--list"), "v1.0.0") {
		t.Error("tag not created in git")
	}
	mustOK(t, op(t, srv, "/api/repo/r/tag", map[string]any{"action": "delete", "name": "v1.0.0"}))
	if strings.TrimSpace(git(t, dir, "tag", "--list")) != "" {
		t.Error("tag not deleted")
	}
}

func TestHistoryGraph(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	git(t, dir, "checkout", "-q", "-b", "dev")
	commitFile(t, dir, "b.txt", "b\n", "add b")
	git(t, dir, "checkout", "-q", "main")
	commitFile(t, dir, "c.txt", "c\n", "add c")
	git(t, dir, "merge", "-q", "--no-ff", "-m", "merge dev", "dev")

	srv := newServer(t, root)
	d := getObj(t, srv, "/api/repo/r/history?limit=20")
	commits := d["commits"].([]any)
	if len(commits) < 4 {
		t.Fatalf("want >=4 commits, got %d", len(commits))
	}
	// newest commit is the merge: two parents
	top := commits[0].(map[string]any)
	if len(toStrSlice(top["parents"])) != 2 {
		t.Errorf("merge commit should have 2 parents, got %v", top["parents"])
	}
	if !contains(toStrSlice(top["refs"]), "main") {
		t.Errorf("top commit refs should include main, got %v", top["refs"])
	}
}

func TestDiscardPatchLine(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	commitFile(t, dir, "f.txt", "a\nb\nc\n", "base")
	write(t, dir, "f.txt", "a\nB\nc\n") // b -> B

	srv := newServer(t, root)
	fd := getObj(t, srv, "/api/repo/r/diff?path=f.txt")
	patch := buildTestPatch(fd, func(tt, c string) bool { return true })

	mustOK(t, op(t, srv, "/api/repo/r/discard-patch", map[string]any{"patch": patch}))
	if strings.TrimSpace(readFile(t, dir, "f.txt")) != "a\nb\nc" {
		t.Errorf("discard-patch did not restore the line: %q", readFile(t, dir, "f.txt"))
	}
}

func TestExpandContext(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "line" + string(rune('A'+i%26))
	}
	commitFile(t, dir, "f.txt", strings.Join(lines, "\n")+"\n", "base")
	lines[20] = "CHANGED"
	write(t, dir, "f.txt", strings.Join(lines, "\n")+"\n")

	srv := newServer(t, root)
	small := countDiffLines(getObj(t, srv, "/api/repo/r/diff?path=f.txt&context=3"))
	big := countDiffLines(getObj(t, srv, "/api/repo/r/diff?path=f.txt&context=50"))
	if big <= small {
		t.Errorf("expand context did not grow the diff: small=%d big=%d", small, big)
	}
}

func TestNoNewlineDiff(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	commitFile(t, dir, "f.txt", "a\nb\nc\n", "base")
	write(t, dir, "f.txt", "a\nb\nC") // change last line + drop trailing newline

	srv := newServer(t, root)
	fd := getObj(t, srv, "/api/repo/r/diff?path=f.txt")
	found := false
	for _, hraw := range fd["hunks"].([]any) {
		for _, lraw := range hraw.(map[string]any)["lines"].([]any) {
			l := lraw.(map[string]any)
			if l["c"] == "C" && l["noNL"] == true {
				found = true
			}
		}
	}
	if !found {
		t.Error("diff did not flag the no-newline line (noNL marker lost)")
	}
}

// TestNoNewlineAppendCommit covers the common no-trailing-newline case: appending
// a final line with no trailing newline must commit and PRESERVE the lack of a
// trailing newline (a dropped marker would silently re-add one).
func TestNoNewlineAppendCommit(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	git(t, dir, "rm", "-q", "README.md")
	commitFile(t, dir, "f.txt", "a\nb\nc\n", "base")
	write(t, dir, "f.txt", "a\nb\nc\nd") // append d, no trailing newline

	srv := newServer(t, root)
	fd := getObj(t, srv, "/api/repo/r/diff?path=f.txt")
	patch := buildTestPatch(fd, func(tt, c string) bool { return true })
	mustOK(t, op(t, srv, "/api/repo/r/commit", map[string]any{
		"message": "append nonl",
		"files":   []map[string]any{{"path": "f.txt", "mode": "patch", "patch": patch}},
	}))
	if got := git(t, dir, "show", "HEAD:f.txt"); got != "a\nb\nc\nd" {
		t.Fatalf("no-newline append: committed %q, want exactly \"a\\nb\\nc\\nd\" (no trailing newline)", got)
	}
}

// TestNoNewlinePartialNoCorruption guards the high-severity bug: deselecting the
// deletion of a no-trailing-newline EOF line used to re-emit the marker mid-hunk
// and SILENTLY CORRUPT the file. The fix never emits a mid-hunk marker, so the
// outcome is always either a correct commit or a clean rejection — never glued
// lines and never a changed HEAD on failure.
func TestNoNewlinePartialNoCorruption(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	git(t, dir, "rm", "-q", "README.md")
	write(t, dir, "f.txt", "a\nb") // committed WITHOUT a trailing newline
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "base")
	write(t, dir, "f.txt", "a\nB\nc\n")

	srv := newServer(t, root)
	fd := getObj(t, srv, "/api/repo/r/diff?path=f.txt")
	patch := buildTestPatch(fd, func(tt, c string) bool { return !(tt == "-" && c == "b") })

	r := op(t, srv, "/api/repo/r/commit", map[string]any{
		"message": "partial nonl",
		"files":   []map[string]any{{"path": "f.txt", "mode": "patch", "patch": patch}},
	})
	got := git(t, dir, "show", "HEAD:f.txt")
	if strings.Contains(got, "bB") { // glued lines = the old corruption signature
		t.Fatalf("patch corrupted the committed file: %q", got)
	}
	if !r.OK && strings.TrimSpace(got) != "a\nb" {
		t.Fatalf("a rejected commit must leave HEAD intact, got %q", got)
	}
}

func TestBulkCommitCheckout(t *testing.T) {
	root := t.TempDir()
	r1 := initRepo(t, root, "r1")
	r2 := initRepo(t, root, "r2")
	write(t, r1, "a.txt", "1\n")
	write(t, r2, "a.txt", "2\n")

	srv := newServer(t, root)
	res := bulkResults(t, srv, "/api/bulk/commit", map[string]any{
		"repos": []string{"r1", "r2"}, "message": "same message",
	})
	if len(res) != 2 {
		t.Fatalf("want 2 results, got %d", len(res))
	}
	for _, dir := range []string{r1, r2} {
		if strings.TrimSpace(git(t, dir, "log", "-1", "--format=%s")) != "same message" {
			t.Errorf("%s did not get the bulk commit", dir)
		}
	}

	bulkResults(t, srv, "/api/bulk/checkout", map[string]any{
		"repos": []string{"r1", "r2"}, "branch": "release", "create": true,
	})
	for _, dir := range []string{r1, r2} {
		if strings.TrimSpace(git(t, dir, "rev-parse", "--abbrev-ref", "HEAD")) != "release" {
			t.Errorf("%s not on release branch", dir)
		}
	}
}

func TestCrossRepoSearch(t *testing.T) {
	root := t.TempDir()
	r1 := initRepo(t, root, "r1")
	r2 := initRepo(t, root, "r2")
	commitFile(t, r1, "x.go", "package x // UNIQUETOKEN here\n", "x")
	commitFile(t, r2, "y.go", "package y // nothing\n", "y")

	srv := newServer(t, root)
	d := getObj(t, srv, "/api/search?q=UNIQUETOKEN")
	hits := d["hits"].([]any)
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d: %v", len(hits), hits)
	}
	if hits[0].(map[string]any)["repo"] != "r1" {
		t.Errorf("hit repo = %v, want r1", hits[0].(map[string]any)["repo"])
	}
}

func TestSnapshot(t *testing.T) {
	root := t.TempDir()
	r1 := initRepo(t, root, "r1")
	git(t, r1, "checkout", "-q", "-b", "feature")

	srv := newServer(t, root)
	snap := getObj(t, srv, "/api/snapshot")["entries"].([]any)
	if len(snap) != 1 || snap[0].(map[string]any)["branch"] != "feature" {
		t.Fatalf("snapshot = %v", snap)
	}
	// move away, then restore
	git(t, r1, "checkout", "-q", "-b", "other")
	mustOKBulk(t, bulkResults(t, srv, "/api/snapshot/restore", map[string]any{"entries": snap}))
	if strings.TrimSpace(git(t, r1, "rev-parse", "--abbrev-ref", "HEAD")) != "feature" {
		t.Error("restore did not return to the saved branch")
	}
}

func TestDiscardToStash(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	write(t, dir, "README.md", "hello\nedited\n")

	srv := newServer(t, root)
	mustOK(t, op(t, srv, "/api/repo/r/discard", map[string]any{"toStash": true}))
	if git(t, dir, "status", "--porcelain") != "" {
		t.Error("discard-to-stash did not clean the tree")
	}
	if !strings.Contains(git(t, dir, "stash", "list"), "discarded") {
		t.Error("discard-to-stash did not create a recoverable stash")
	}
}

func TestSettingsPersist(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root, "r")
	srv := newServer(t, root)

	out, code := req(t, srv, "POST", "/api/settings", map[string]any{
		"theme": "light", "lang": "fa", "defaultPull": "rebase", "fontSize": 16,
	})
	if code != 200 {
		t.Fatalf("set settings -> %d: %s", code, out)
	}
	s := getObj(t, srv, "/api/settings")
	if s["theme"] != "light" || s["lang"] != "fa" || s["defaultPull"] != "rebase" {
		t.Errorf("settings not persisted: %v", s)
	}
}

// ---- helpers ----

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func countDiffLines(fd map[string]any) int {
	n := 0
	for _, hraw := range fd["hunks"].([]any) {
		n += len(hraw.(map[string]any)["lines"].([]any))
	}
	return n
}

func bulkResults(t *testing.T, srv *httptest.Server, path string, body any) []OpResult {
	t.Helper()
	out, code := req(t, srv, "POST", path, body)
	if code != 200 {
		t.Fatalf("POST %s -> %d: %s", path, code, out)
	}
	var d struct {
		Results []OpResult `json:"results"`
	}
	if err := json.Unmarshal(out, &d); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, out)
	}
	return d.Results
}

func mustOKBulk(t *testing.T, rs []OpResult) {
	t.Helper()
	for _, r := range rs {
		if !r.OK {
			t.Fatalf("%s %s failed: %s", r.Action, r.Repo, r.Output)
		}
	}
}

// TestResetModes exercises soft / mixed / hard reset and the safety backup
// branch on throwaway temp repos — never a real working branch.
func TestResetModes(t *testing.T) {
	root := t.TempDir()

	// build makes name/ with commits c1..c3 touching f.txt; returns dir, c1, c3.
	build := func(name string) (dir, c1, c3 string) {
		dir = initRepo(t, root, name)
		commitFile(t, dir, "f.txt", "v1\n", "c1")
		c1 = strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
		commitFile(t, dir, "f.txt", "v1\nv2\n", "c2")
		commitFile(t, dir, "f.txt", "v1\nv2\nv3\n", "c3")
		c3 = strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
		return
	}
	read := func(dir string) string {
		b, err := os.ReadFile(filepath.Join(dir, "f.txt"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	soft, softC1, _ := build("soft")
	mixed, mixedC1, _ := build("mixed")
	hard, hardC1, hardC3 := build("hard")
	plain, plainC1, plainC3 := build("plain")
	bad, _, _ := build("bad")

	srv := newServer(t, root)

	// --- soft: HEAD moves; later work kept STAGED; working tree untouched ---
	mustOK(t, op(t, srv, "/api/repo/soft/reset", map[string]any{"hash": softC1, "mode": "soft"}))
	if head := strings.TrimSpace(git(t, soft, "rev-parse", "HEAD")); head != softC1 {
		t.Fatalf("soft: HEAD = %s, want %s", head, softC1)
	}
	if got := read(soft); got != "v1\nv2\nv3\n" {
		t.Errorf("soft must not touch the working tree, got %q", got)
	}
	if staged := git(t, soft, "diff", "--cached", "--name-only"); !strings.Contains(staged, "f.txt") {
		t.Error("soft must keep later changes staged")
	}

	// --- mixed: HEAD moves; later work kept UNSTAGED in the working tree ---
	mustOK(t, op(t, srv, "/api/repo/mixed/reset", map[string]any{"hash": mixedC1, "mode": "mixed"}))
	if head := strings.TrimSpace(git(t, mixed, "rev-parse", "HEAD")); head != mixedC1 {
		t.Fatalf("mixed: HEAD = %s, want %s", head, mixedC1)
	}
	if got := read(mixed); got != "v1\nv2\nv3\n" {
		t.Errorf("mixed must not touch the working tree, got %q", got)
	}
	if staged := strings.TrimSpace(git(t, mixed, "diff", "--cached", "--name-only")); staged != "" {
		t.Errorf("mixed must stage nothing, got %q", staged)
	}
	if un := git(t, mixed, "diff", "--name-only"); !strings.Contains(un, "f.txt") {
		t.Error("mixed must leave later changes unstaged")
	}

	// --- hard + backup: later work discarded, but a safety branch keeps it ---
	r := op(t, srv, "/api/repo/hard/reset", map[string]any{"hash": hardC1, "mode": "hard", "backup": true})
	mustOK(t, r)
	if head := strings.TrimSpace(git(t, hard, "rev-parse", "HEAD")); head != hardC1 {
		t.Fatalf("hard: HEAD = %s, want %s", head, hardC1)
	}
	if got := read(hard); got != "v1\n" {
		t.Errorf("hard must discard later work, got %q", got)
	}
	branches := strings.TrimSpace(git(t, hard, "branch", "--list", "chithub/backup-*"))
	if branches == "" {
		t.Fatal("hard+backup must create a chithub/backup-* safety branch")
	}
	backupName := strings.TrimSpace(strings.TrimPrefix(branches, "*"))
	if tip := strings.TrimSpace(git(t, hard, "rev-parse", backupName)); tip != hardC3 {
		t.Errorf("safety branch points at %s, want previous tip %s", tip, hardC3)
	}
	if !strings.Contains(r.Output, "Safety branch") {
		t.Errorf("output should mention the safety branch: %q", r.Output)
	}

	// --- hard WITHOUT backup: still works; no safety branch; reflog hint shown ---
	r2 := op(t, srv, "/api/repo/plain/reset", map[string]any{"hash": plainC1, "mode": "hard"})
	mustOK(t, r2)
	if head := strings.TrimSpace(git(t, plain, "rev-parse", "HEAD")); head != plainC1 {
		t.Fatalf("plain hard: HEAD = %s, want %s", head, plainC1)
	}
	if b := strings.TrimSpace(git(t, plain, "branch", "--list", "chithub/backup-*")); b != "" {
		t.Errorf("no backup requested, but a safety branch appeared: %q", b)
	}
	// the dropped tip must still be reachable from the reflog
	if !strings.Contains(git(t, plain, "reflog"), plainC3[:7]) {
		t.Error("dropped commit should remain in the reflog")
	}

	// --- invalid mode is rejected and never moves HEAD ---
	before := strings.TrimSpace(git(t, bad, "rev-parse", "HEAD"))
	rb := op(t, srv, "/api/repo/bad/reset", map[string]any{"hash": before, "mode": "nope"})
	if rb.OK {
		t.Error("invalid reset mode must fail")
	}
	if after := strings.TrimSpace(git(t, bad, "rev-parse", "HEAD")); after != before {
		t.Error("a rejected reset must not move HEAD")
	}
}
