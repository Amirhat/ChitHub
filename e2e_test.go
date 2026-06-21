package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ===========================================================================
// End-to-end tests: spin up the real HTTP API against throwaway git repos and
// assert the resulting git state after each operation. Run with `go test ./...`.
// Everything is local (bare repos act as remotes) so it works offline.
// ===========================================================================

// ---- helpers ----

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t.io",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t.io",
		"GIT_CONFIG_GLOBAL="+filepath.Join(t.TempDir(), "gitconfig"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initRepo makes root/name a git repo on branch main with one commit.
func initRepo(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "README.md", "hello\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

func newServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home) // isolate ~/.chithub.json AND the git identity used by app commits
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"),
		[]byte("[user]\n\tname = ChitHub Test\n\temail = test@chithub.local\n[init]\n\tdefaultBranch = main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: Config{Collections: []string{root}, Active: root, Port: "0"}}
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func req(t *testing.T, srv *httptest.Server, method, path string, body any) ([]byte, int) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	hr, _ := http.NewRequest(method, srv.URL+path, r)
	resp, err := http.DefaultClient.Do(hr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return out, resp.StatusCode
}

func getObj(t *testing.T, srv *httptest.Server, path string) map[string]any {
	t.Helper()
	out, code := req(t, srv, "GET", path, nil)
	if code != 200 {
		t.Fatalf("GET %s -> %d: %s", path, code, out)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("GET %s decode: %v\n%s", path, err, out)
	}
	return m
}

func op(t *testing.T, srv *httptest.Server, path string, body any) OpResult {
	t.Helper()
	out, code := req(t, srv, "POST", path, body)
	if code != 200 {
		t.Fatalf("POST %s -> %d: %s", path, code, out)
	}
	var r OpResult
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("POST %s decode: %v\n%s", path, err, out)
	}
	return r
}

func mustOK(t *testing.T, r OpResult) {
	t.Helper()
	if !r.OK {
		t.Fatalf("%s %s failed: %s", r.Action, r.Repo, r.Output)
	}
}

func repoFromList(t *testing.T, srv *httptest.Server, name string) map[string]any {
	t.Helper()
	d := getObj(t, srv, "/api/repos")
	for _, ri := range d["repos"].([]any) {
		r := ri.(map[string]any)
		if r["name"] == name {
			return r
		}
	}
	t.Fatalf("repo %s not found in /api/repos", name)
	return nil
}

func num(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

// ---- tests ----

func TestStatusAndScan(t *testing.T) {
	root := t.TempDir()
	clean := initRepo(t, root, "clean")
	dirty := initRepo(t, root, "dirty")
	_ = clean

	// make 'dirty' dirty: modify tracked + add untracked
	write(t, dirty, "README.md", "hello\nchanged\n")
	write(t, dirty, "new.txt", "x\n")

	srv := newServer(t, root)
	d := getObj(t, srv, "/api/repos")
	repos := d["repos"].([]any)
	if len(repos) != 2 {
		t.Fatalf("want 2 repos, got %d", len(repos))
	}

	cleanInfo := repoFromList(t, srv, "clean")
	if cleanInfo["dirty"].(bool) {
		t.Error("clean repo reported dirty")
	}
	if cleanInfo["state"] != "no-upstream" {
		t.Errorf("clean state = %v, want no-upstream", cleanInfo["state"])
	}

	dirtyInfo := repoFromList(t, srv, "dirty")
	if !dirtyInfo["dirty"].(bool) {
		t.Error("dirty repo not reported dirty")
	}
	if num(dirtyInfo["unstaged"]) != 1 || num(dirtyInfo["untracked"]) != 1 {
		t.Errorf("dirty counts: unstaged=%d untracked=%d, want 1/1", num(dirtyInfo["unstaged"]), num(dirtyInfo["untracked"]))
	}
}

func TestDiffAndWholeFileCommit(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	write(t, dir, "README.md", "hello\nworld\n")
	srv := newServer(t, root)

	// diff endpoint exposes a preamble + hunks
	d := getObj(t, srv, "/api/repo/r/diff?path=README.md")
	if d["preamble"].(string) == "" {
		t.Error("diff preamble is empty")
	}
	if len(d["hunks"].([]any)) == 0 {
		t.Error("diff has no hunks")
	}

	r := op(t, srv, "/api/repo/r/commit", map[string]any{
		"message": "whole file",
		"files":   []map[string]any{{"path": "README.md", "mode": "all"}},
	})
	mustOK(t, r)
	if got := strings.TrimSpace(git(t, dir, "show", "HEAD:README.md")); got != "hello\nworld" {
		t.Errorf("committed content = %q", got)
	}
	if git(t, dir, "status", "--porcelain") != "" {
		t.Error("working tree not clean after whole-file commit")
	}
}

// TestLineLevelCommit is the critical one: build a patch from a subset of the
// diff lines (exactly as the web UI does) and verify only those lines land.
func TestLineLevelCommit(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	git(t, dir, "rm", "-q", "README.md")
	write(t, dir, "f.txt", "a\nb\nc\nd\ne\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "base")
	// change line b->B (keep), add NEW (drop), delete d (keep)
	write(t, dir, "f.txt", "a\nB\nNEW\nc\ne\n")

	srv := newServer(t, root)
	fd := getObj(t, srv, "/api/repo/r/diff?path=f.txt")

	// Keep every changed line EXCEPT the added "NEW".
	patch := buildTestPatch(fd, func(t string, c string) bool {
		return !(t == "+" && c == "NEW")
	})
	if patch == "" {
		t.Fatal("empty patch built")
	}

	r := op(t, srv, "/api/repo/r/commit", map[string]any{
		"message": "line level",
		"files":   []map[string]any{{"path": "f.txt", "mode": "patch", "patch": patch}},
	})
	mustOK(t, r)

	// HEAD should have b->B and d removed, but NOT the NEW line.
	got := strings.TrimSpace(git(t, dir, "show", "HEAD:f.txt"))
	if got != "a\nB\nc\ne" {
		t.Fatalf("committed = %q, want a/B/c/e", got)
	}
	// The dropped "NEW" line must still be an unstaged change.
	if !strings.Contains(git(t, dir, "diff"), "+NEW") {
		t.Error("the deselected NEW line should remain unstaged")
	}
}

// buildTestPatch mirrors the web UI's buildFilePatch: unselected additions are
// dropped, unselected deletions become context, counts recomputed.
func buildTestPatch(fd map[string]any, keep func(t, c string) bool) string {
	pre, _ := fd["preamble"].(string)
	var body strings.Builder
	re := regexp.MustCompile(`@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
	for _, hraw := range fd["hunks"].([]any) {
		h := hraw.(map[string]any)
		var oldc, newc int
		var has bool
		var lines []string
		for _, lraw := range h["lines"].([]any) {
			l := lraw.(map[string]any)
			tt, _ := l["t"].(string)
			c, _ := l["c"].(string)
			switch tt {
			case " ":
				lines = append(lines, " "+c)
				oldc++
				newc++
			case "+":
				if keep(tt, c) {
					lines = append(lines, "+"+c)
					newc++
					has = true
				}
			case "-":
				if keep(tt, c) {
					lines = append(lines, "-"+c)
					oldc++
					has = true
				} else {
					lines = append(lines, " "+c)
					oldc++
					newc++
				}
			}
		}
		if !has {
			continue
		}
		m := re.FindStringSubmatch(h["header"].(string))
		os_, ns := "1", "1"
		if m != nil {
			os_, ns = m[1], m[2]
		}
		body.WriteString(fmt.Sprintf("@@ -%s,%d +%s,%d @@\n", os_, oldc, ns, newc))
		body.WriteString(strings.Join(lines, "\n") + "\n")
	}
	if body.Len() == 0 {
		return ""
	}
	if !strings.HasSuffix(pre, "\n") {
		pre += "\n"
	}
	return pre + body.String()
}

func TestAmendAndUndo(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	srv := newServer(t, root)

	// make a commit through the API
	write(t, dir, "a.txt", "1\n")
	mustOK(t, op(t, srv, "/api/repo/r/commit", map[string]any{
		"message": "add a", "files": []map[string]any{{"path": "a.txt", "mode": "all"}},
	}))
	first := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	// amend: reword + fold a new file in
	write(t, dir, "b.txt", "2\n")
	mustOK(t, op(t, srv, "/api/repo/r/amend", map[string]any{
		"message": "add a and b", "files": []map[string]any{{"path": "b.txt", "mode": "all"}},
	}))
	if strings.TrimSpace(git(t, dir, "log", "-1", "--format=%s")) != "add a and b" {
		t.Error("amend did not reword")
	}
	if strings.TrimSpace(git(t, dir, "rev-parse", "HEAD")) == first {
		t.Error("amend did not rewrite HEAD")
	}
	if !strings.Contains(git(t, dir, "show", "--stat", "HEAD"), "b.txt") {
		t.Error("amend did not fold b.txt into the commit")
	}

	// undo last commit (soft) -> changes return to working tree
	mustOK(t, op(t, srv, "/api/repo/r/undo", nil))
	if strings.TrimSpace(git(t, dir, "log", "-1", "--format=%s")) != "init" {
		t.Error("undo did not move HEAD back")
	}
	if git(t, dir, "status", "--porcelain") == "" {
		t.Error("undo should have left changes in the tree")
	}
}

func TestStashAndDiscard(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	srv := newServer(t, root)

	write(t, dir, "README.md", "hello\nedited\n")
	write(t, dir, "untracked.txt", "u\n")

	// stash push -> tree clean
	mustOK(t, op(t, srv, "/api/repo/r/stash", map[string]any{"action": "push"}))
	if git(t, dir, "status", "--porcelain") != "" {
		t.Error("stash push did not clean the tree")
	}
	// stash pop -> changes back
	mustOK(t, op(t, srv, "/api/repo/r/stash", map[string]any{"action": "pop"}))
	if !strings.Contains(git(t, dir, "status", "--porcelain"), "README.md") {
		t.Error("stash pop did not restore changes")
	}

	// discard only README.md (untracked.txt must survive)
	mustOK(t, op(t, srv, "/api/repo/r/discard", map[string]any{"paths": []string{"README.md"}}))
	st := git(t, dir, "status", "--porcelain")
	if strings.Contains(st, "README.md") {
		t.Error("README.md change should be discarded")
	}
	if !strings.Contains(st, "untracked.txt") {
		t.Error("untracked.txt should still be present")
	}
}

func TestBranchSwitchWithStash(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	srv := newServer(t, root)

	// create a feature branch and add a file there
	mustOK(t, op(t, srv, "/api/repo/r/checkout", map[string]any{"branch": "feature", "create": true}))
	if strings.TrimSpace(git(t, dir, "rev-parse", "--abbrev-ref", "HEAD")) != "feature" {
		t.Fatal("did not switch to feature")
	}
	write(t, dir, "feat.txt", "feature\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "feat")
	// back to main
	mustOK(t, op(t, srv, "/api/repo/r/checkout", map[string]any{"branch": "main"}))

	// make a conflicting local change to README.md, then try to switch with stash
	write(t, dir, "README.md", "hello\nmain-edit\n")
	// first, a plain checkout to a branch that also touches README would conflict;
	// here just verify stash:true cleanly switches and stashes.
	r := op(t, srv, "/api/repo/r/checkout", map[string]any{"branch": "feature", "stash": true})
	mustOK(t, r)
	if strings.TrimSpace(git(t, dir, "rev-parse", "--abbrev-ref", "HEAD")) != "feature" {
		t.Error("stash-switch did not land on feature")
	}
	if !strings.Contains(git(t, dir, "stash", "list"), "auto-stash") {
		t.Error("expected an auto-stash entry to exist")
	}
}

func TestCloneFetchPull(t *testing.T) {
	root := t.TempDir()
	remotes := t.TempDir()

	// build a bare remote with two commits, via a seed clone
	bare := filepath.Join(remotes, "origin.git")
	git(t, remotes, "init", "-q", "--bare", "-b", "main", bare)
	seed := filepath.Join(remotes, "seed")
	git(t, remotes, "clone", "-q", bare, seed)
	write(t, seed, "app.txt", "v1\n")
	git(t, seed, "add", "-A")
	git(t, seed, "commit", "-qm", "v1")
	git(t, seed, "push", "-q", "origin", "main")

	srv := newServer(t, root)

	// clone into the collection through the API
	mustOK(t, op(t, srv, "/api/clone", map[string]any{"url": bare, "name": "work"}))
	work := filepath.Join(root, "work")
	if _, err := os.Stat(filepath.Join(work, ".git")); err != nil {
		t.Fatal("clone did not create work/.git")
	}

	// advance the remote
	write(t, seed, "app.txt", "v1\nv2\n")
	git(t, seed, "commit", "-qam", "v2")
	git(t, seed, "push", "-q", "origin", "main")

	// fetch -> work should report behind=1
	mustOK(t, op(t, srv, "/api/repo/work/fetch", nil))
	info := repoFromList(t, srv, "work")
	if num(info["behind"]) != 1 {
		t.Fatalf("behind = %d, want 1", num(info["behind"]))
	}

	// pull (ff) -> up to date
	mustOK(t, op(t, srv, "/api/repo/work/pull", map[string]any{"mode": "ff"}))
	info = repoFromList(t, srv, "work")
	if num(info["behind"]) != 0 || info["state"] != "synced" {
		t.Fatalf("after pull behind=%d state=%v", num(info["behind"]), info["state"])
	}
}

func TestPush(t *testing.T) {
	root := t.TempDir()
	remotes := t.TempDir()
	bare := filepath.Join(remotes, "origin.git")
	git(t, remotes, "init", "-q", "--bare", "-b", "main", bare)

	work := initRepo(t, root, "work")
	git(t, work, "remote", "add", "origin", bare)
	git(t, work, "push", "-q", "-u", "origin", "main")

	srv := newServer(t, root)

	// local commit, then push via API
	write(t, work, "x.txt", "x\n")
	mustOK(t, op(t, srv, "/api/repo/work/commit", map[string]any{
		"message": "x", "files": []map[string]any{{"path": "x.txt", "mode": "all"}},
	}))
	info := repoFromList(t, srv, "work")
	if num(info["ahead"]) != 1 {
		t.Fatalf("ahead = %d, want 1 before push", num(info["ahead"]))
	}
	mustOK(t, op(t, srv, "/api/repo/work/push", map[string]any{"force": false}))
	info = repoFromList(t, srv, "work")
	if num(info["ahead"]) != 0 {
		t.Fatalf("ahead = %d, want 0 after push", num(info["ahead"]))
	}
	// the bare remote really has the new file
	if !strings.Contains(git(t, remotes, "--git-dir", bare, "log", "--oneline"), "x") {
		t.Error("remote did not receive the pushed commit")
	}
}

func TestCollections(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	initRepo(t, a, "ra")
	initRepo(t, b, "rb1")
	initRepo(t, b, "rb2")

	srv := newServer(t, a)

	// add b, becomes active
	out, code := req(t, srv, "POST", "/api/collections", map[string]any{"action": "add", "path": b})
	if code != 200 {
		t.Fatalf("add collection -> %d: %s", code, out)
	}
	var res map[string]any
	json.Unmarshal(out, &res)
	if res["active"] != b {
		t.Errorf("active = %v, want %v", res["active"], b)
	}
	if len(res["collections"].([]any)) != 2 {
		t.Errorf("want 2 collections, got %d", len(res["collections"].([]any)))
	}
	// active scan now shows b's repos
	if len(getObj(t, srv, "/api/repos")["repos"].([]any)) != 2 {
		t.Error("active collection should show 2 repos (b)")
	}

	// switch back to a
	req(t, srv, "POST", "/api/collections", map[string]any{"action": "switch", "path": a})
	if len(getObj(t, srv, "/api/repos")["repos"].([]any)) != 1 {
		t.Error("after switch to a, want 1 repo")
	}

	// remove b
	out, _ = req(t, srv, "POST", "/api/collections", map[string]any{"action": "remove", "path": b})
	json.Unmarshal(out, &res)
	if len(res["collections"].([]any)) != 1 {
		t.Errorf("after remove want 1 collection, got %d", len(res["collections"].([]any)))
	}
}

func TestReviewQueues(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root, "clean")
	dirty := initRepo(t, root, "dirty")
	write(t, dirty, "z.txt", "z\n")

	srv := newServer(t, root)
	d := getObj(t, srv, "/api/review")
	commit := toStrSlice(d["commit"])
	if !contains(commit, "dirty") {
		t.Errorf("commit queue %v should include dirty", commit)
	}
	if contains(commit, "clean") {
		t.Errorf("commit queue %v should not include clean", commit)
	}
}

func TestCommitShowIncludingMerge(t *testing.T) {
	root := t.TempDir()
	dir := initRepo(t, root, "r")
	// regular commit
	write(t, dir, "a.txt", "1\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "add a")
	reg := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	// build a merge: branch dev adds b.txt, merge back with --no-ff
	git(t, dir, "checkout", "-q", "-b", "dev")
	write(t, dir, "b.txt", "2\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "add b")
	git(t, dir, "checkout", "-q", "main")
	git(t, dir, "merge", "-q", "--no-ff", "-m", "merge dev", "dev")
	merge := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	srv := newServer(t, root)

	// regular commit shows its file
	d := getObj(t, srv, "/api/repo/r/show?hash="+reg)
	if len(d["files"].([]any)) == 0 {
		t.Error("regular commit show returned no files")
	}
	// merge commit shows files via --first-parent (not empty / not an error)
	d = getObj(t, srv, "/api/repo/r/show?hash="+merge)
	if len(d["files"].([]any)) == 0 {
		t.Error("merge commit show returned no files (--first-parent regression)")
	}
}

func toStrSlice(v any) []string {
	var out []string
	if arr, ok := v.([]any); ok {
		for _, e := range arr {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func contains(s []string, x string) bool {
	for _, e := range s {
		if e == x {
			return true
		}
	}
	return false
}
