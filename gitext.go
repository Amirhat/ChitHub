package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ---- types ----

type DiffLine struct {
	T string `json:"t"` // " " context, "+" add, "-" del
	C string `json:"c"`
}

type Hunk struct {
	Index  int        `json:"index"`
	Header string     `json:"header"`
	Lines  []DiffLine `json:"lines"`
}

type FileDiff struct {
	Path      string `json:"path"`
	Binary    bool   `json:"binary"`
	TooLarge  bool   `json:"tooLarge"`
	Untracked bool   `json:"untracked"`
	Preamble  string `json:"preamble"` // diff header (before the first @@), for rebuilding patches
	Hunks     []Hunk `json:"hunks"`
}

type BranchInfo struct {
	Current string   `json:"current"`
	Local   []string `json:"local"`
	Remote  []string `json:"remote"`
}

// CommitFile is one selected entry in a selective commit. Mode "all" stages the
// whole file; mode "patch" applies the supplied unified diff (built client-side
// from the exact hunks/lines the user ticked) to the index.
type CommitFile struct {
	Path  string `json:"path"`
	Mode  string `json:"mode"`  // "all" | "patch"
	Patch string `json:"patch"` // unified diff for mode "patch"
}

// ---- branches ----

func repoBranches(root, name string) BranchInfo {
	dir := filepath.Join(root, name)
	var b BranchInfo
	if out, err := runGit(dir, 5*time.Second, "branch", "--format=%(refname:short)"); err == nil {
		for _, l := range splitLines(out) {
			b.Local = append(b.Local, l)
		}
	}
	if out, err := runGit(dir, 5*time.Second, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		b.Current = strings.TrimSpace(out)
	}
	if out, err := runGit(dir, 5*time.Second, "branch", "-r", "--format=%(refname:short)"); err == nil {
		for _, l := range splitLines(out) {
			if strings.HasSuffix(l, "/HEAD") || strings.Contains(l, "->") {
				continue
			}
			b.Remote = append(b.Remote, l)
		}
	}
	return b
}

func checkoutBranch(root, name, branch string, create bool, startPoint string, stash bool) OpResult {
	dir := filepath.Join(root, name)
	var pre string
	if stash {
		o, e := runGit(dir, 30*time.Second, "stash", "push", "-u", "-m",
			"chithub: auto-stash before switching to "+branch)
		if e != nil {
			return mkResult(name, "checkout", o, e)
		}
		pre = strings.TrimSpace(o)
	}
	args := []string{"checkout"}
	if create {
		args = append(args, "-b", branch)
		if strings.TrimSpace(startPoint) != "" {
			args = append(args, startPoint)
		}
	} else {
		args = append(args, branch)
	}
	out, err := runGit(dir, 30*time.Second, args...)
	res := mkResult(name, "checkout", out, err)
	if pre != "" && res.OK {
		res.Output = strings.TrimSpace(pre + "\n" + res.Output +
			"\n(your changes were stashed — pop them with the Stash button)")
	}
	return res
}

// ---- discard ----

func discardChanges(root, name string, paths []string) OpResult {
	dir := filepath.Join(root, name)
	var out strings.Builder
	ok := true

	if len(paths) == 0 {
		o1, e1 := runGit(dir, 30*time.Second, "checkout", "HEAD", "--", ".")
		o2, e2 := runGit(dir, 30*time.Second, "clean", "-fd")
		out.WriteString(o1)
		out.WriteString(o2)
		ok = e1 == nil && e2 == nil
	} else {
		for _, p := range paths {
			if isUntracked(dir, p) {
				o, e := runGit(dir, 15*time.Second, "clean", "-fd", "--", p)
				out.WriteString(o)
				if e != nil {
					ok = false
				}
			} else {
				o, e := runGit(dir, 15*time.Second, "checkout", "HEAD", "--", p)
				out.WriteString(o)
				if e != nil {
					ok = false
				}
			}
		}
	}
	res := OpResult{Repo: name, Action: "discard", Output: strings.TrimSpace(out.String()), OK: ok}
	if ok && res.Output == "" {
		res.Output = "Discarded."
	}
	return res
}

func isUntracked(dir, p string) bool {
	out, err := runGit(dir, 5*time.Second, "status", "--porcelain", "--", p)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(out), "??")
}

// ---- stash ----

func stashOp(root, name, action, message string) OpResult {
	dir := filepath.Join(root, name)
	var args []string
	switch action {
	case "pop":
		args = []string{"stash", "pop"}
	case "apply":
		args = []string{"stash", "apply"}
	case "drop":
		args = []string{"stash", "drop"}
	default: // push
		args = []string{"stash", "push", "-u"}
		if strings.TrimSpace(message) != "" {
			args = append(args, "-m", message)
		}
	}
	out, err := runGit(dir, 30*time.Second, args...)
	return mkResult(name, "stash", out, err)
}

func stashList(root, name string) []string {
	out, err := runGit(filepath.Join(root, name), 5*time.Second, "stash", "list")
	if err != nil {
		return nil
	}
	return splitLines(out)
}

// ---- clone ----

func cloneRepo(root, url, name string) OpResult {
	args := []string{"clone", url}
	if strings.TrimSpace(name) != "" {
		args = append(args, name)
	}
	out, err := runGit(root, 600*time.Second, args...)
	return mkResult(deriveName(url, name), "clone", out, err)
}

func deriveName(url, name string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	u := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(url), "/"), ".git")
	if i := strings.LastIndexAny(u, "/:"); i >= 0 {
		return u[i+1:]
	}
	return u
}

// ---- diff (display) ----

func fileDiff(root, name, path string) FileDiff {
	dir := filepath.Join(root, name)
	fd := FileDiff{Path: path}

	var out string
	if isUntracked(dir, path) {
		fd.Untracked = true
		out, _ = runGit(dir, 15*time.Second, "diff", "--no-color", "--no-index", "-U3", "--", "/dev/null", path)
	} else {
		o, err := runGit(dir, 15*time.Second, "diff", "--no-color", "-U3", "HEAD", "--", path)
		if err != nil {
			fd.Hunks = nil
			return fd
		}
		out = o
	}

	if strings.Contains(out, "Binary files ") || strings.Contains(out, "GIT binary patch") {
		fd.Binary = true
		return fd
	}
	if len(out) > 600_000 {
		fd.TooLarge = true
		return fd
	}
	preamble, hunks := splitDiffRaw(out)
	fd.Preamble = preamble
	for i, h := range hunks {
		fd.Hunks = append(fd.Hunks, hunkToDisplay(i, h))
	}
	return fd
}

// ---- selective commit ----

// stageSelection rebuilds the index (working tree untouched) to contain exactly
// the selected files/hunks. Returns how many entries were staged, plus output +
// error if any step failed. `git reset` is ignored on an empty repo (no HEAD).
func stageSelection(dir string, files []CommitFile) (int, string, error) {
	_, _ = runGit(dir, 15*time.Second, "reset", "-q")
	touched := 0
	for _, f := range files {
		if f.Mode == "patch" && strings.TrimSpace(f.Patch) != "" {
			patch := f.Patch
			if !strings.HasSuffix(patch, "\n") {
				patch += "\n"
			}
			if o, e := runGitStdin(dir, 20*time.Second, patch,
				"apply", "--cached", "--whitespace=nowarn", "--recount", "-"); e != nil {
				return touched, "failed to stage selection in " + f.Path + ":\n" + o, e
			}
		} else {
			if o, e := runGit(dir, 15*time.Second, "add", "-A", "--", f.Path); e != nil {
				return touched, o, e
			}
		}
		touched++
	}
	return touched, "", nil
}

// commitSelective stages the UI selection, then commits it.
func commitSelective(root, name, msg string, files []CommitFile, push bool) OpResult {
	dir := filepath.Join(root, name)
	touched, out, err := stageSelection(dir, files)
	if err != nil {
		return mkResult(name, "commit", out, err)
	}
	if touched == 0 {
		return OpResult{Repo: name, Action: "commit", OK: false, Output: "No changes selected to commit."}
	}

	cout, cerr := runGit(dir, 30*time.Second, "commit", "-m", msg)
	res := mkResult(name, "commit", cout, cerr)
	if res.OK && push {
		p := pushRepo(root, name, false)
		res.Output = strings.TrimSpace(res.Output + "\n\n" + p.Output)
		res.OK = p.OK
	}
	return res
}

// ---- GitHub-Desktop-style extras ----

// amendCommit folds the selected changes (if any) into the previous commit and
// optionally rewords it. Because it rewrites history, an optional push uses
// --force-with-lease.
func amendCommit(root, name, msg string, files []CommitFile, push bool) OpResult {
	dir := filepath.Join(root, name)
	if len(files) > 0 {
		if _, out, err := stageSelection(dir, files); err != nil {
			return mkResult(name, "amend", out, err)
		}
	}
	args := []string{"commit", "--amend"}
	if strings.TrimSpace(msg) == "" {
		args = append(args, "--no-edit")
	} else {
		args = append(args, "-m", msg)
	}
	out, err := runGit(dir, 30*time.Second, args...)
	res := mkResult(name, "amend", out, err)
	if res.OK && push {
		p := pushRepo(root, name, true)
		res.Output = strings.TrimSpace(res.Output + "\n\n" + p.Output)
		res.OK = p.OK
	}
	return res
}

// undoLastCommit soft-resets HEAD~1, keeping the changes in the working tree.
func undoLastCommit(root, name string) OpResult {
	out, err := runGit(filepath.Join(root, name), 15*time.Second, "reset", "--soft", "HEAD~1")
	r := mkResult(name, "undo", out, err)
	if r.OK && r.Output == "" {
		r.Output = "Last commit undone — its changes are back in your working tree."
	}
	return r
}

// incomingLog lists commits the upstream has that HEAD does not (what a pull
// would bring in). Empty when there is no upstream or nothing to pull.
func incomingLog(root, name string) []CommitInfo {
	out, err := runGit(filepath.Join(root, name), 8*time.Second,
		"log", "-n", "20", "--format=%H%x1f%h%x1f%s%x1f%ct%x1f%an", "HEAD..@{u}")
	if err != nil {
		return nil
	}
	var logs []CommitInfo
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		if cm := parseCommitLine(line); cm != nil {
			logs = append(logs, *cm)
		}
	}
	return logs
}

// commitShow returns the per-file diffs introduced by a commit.
func commitShow(root, name, hash string) []FileDiff {
	if !isHexish(hash) {
		return nil
	}
	out, err := runGit(filepath.Join(root, name), 20*time.Second,
		"show", "--no-color", "-U3", "--format=", hash)
	if err != nil {
		return nil
	}
	return splitMultiFileDiff(out)
}

func isHexish(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func splitMultiFileDiff(out string) []FileDiff {
	var starts []int
	atStart := true
	for i := 0; i < len(out); i++ {
		if atStart && strings.HasPrefix(out[i:], "diff --git ") {
			starts = append(starts, i)
		}
		atStart = out[i] == '\n'
	}
	var files []FileDiff
	for k, s := range starts {
		end := len(out)
		if k+1 < len(starts) {
			end = starts[k+1]
		}
		block := out[s:end]
		fd := FileDiff{Path: parseDiffPath(block)}
		switch {
		case strings.Contains(block, "Binary files ") || strings.Contains(block, "GIT binary patch"):
			fd.Binary = true
		case len(block) > 600_000:
			fd.TooLarge = true
		default:
			_, hunks := splitDiffRaw(block)
			for i, h := range hunks {
				fd.Hunks = append(fd.Hunks, hunkToDisplay(i, h))
			}
		}
		files = append(files, fd)
	}
	return files
}

func parseDiffPath(block string) string {
	first := block
	if nl := strings.IndexByte(block, '\n'); nl >= 0 {
		first = block[:nl]
	}
	parts := strings.Fields(first)
	if len(parts) >= 4 {
		return strings.TrimPrefix(parts[3], "b/")
	}
	return ""
}

// ---- diff parsing helpers ----

// splitDiffRaw splits a unified diff into the file preamble and the exact byte
// slices of each hunk (header line through the line before the next hunk).
func splitDiffRaw(diff string) (string, []string) {
	var starts []int
	atLineStart := true
	for i := 0; i < len(diff); i++ {
		if atLineStart && strings.HasPrefix(diff[i:], "@@ ") {
			starts = append(starts, i)
		}
		atLineStart = diff[i] == '\n'
	}
	if len(starts) == 0 {
		return diff, nil
	}
	preamble := diff[:starts[0]]
	var hunks []string
	for k, s := range starts {
		end := len(diff)
		if k+1 < len(starts) {
			end = starts[k+1]
		}
		hunks = append(hunks, diff[s:end])
	}
	return preamble, hunks
}

func hunkToDisplay(idx int, text string) Hunk {
	lines := strings.Split(text, "\n")
	h := Hunk{Index: idx}
	if len(lines) > 0 {
		h.Header = lines[0]
	}
	for _, bl := range lines[1:] {
		if bl == "" || strings.HasPrefix(bl, "\\") {
			continue
		}
		t := " "
		switch bl[0] {
		case '+':
			t = "+"
		case '-':
			t = "-"
		}
		h.Lines = append(h.Lines, DiffLine{T: t, C: bl[1:]})
	}
	return h
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// ---- OS reveal ----

// pickFolder shows a native folder chooser (macOS) and returns the chosen path.
// Returns an empty string with no error when the user cancels.
func pickFolder() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("osascript", "-e",
			`POSIX path of (choose folder with prompt "Select a folder that contains your repositories")`).Output()
		if err != nil {
			return "", nil // cancelled or no GUI session
		}
		return strings.TrimSpace(string(out)), nil
	default:
		return "", errors.New("native folder picker is only available on macOS")
	}
}

func revealInFinder(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
