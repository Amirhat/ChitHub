package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CommitInfo is a single commit shown in the UI.
type CommitInfo struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Time    int64  `json:"time"` // unix seconds
	Author  string `json:"author"`
}

// FileChange is one entry from `git status`.
type FileChange struct {
	Path   string `json:"path"`
	Code   string `json:"code"` // two-char XY status
	Staged bool   `json:"staged"`
}

// RepoInfo is the full status snapshot for a single repository.
type RepoInfo struct {
	Name        string      `json:"name"`
	Path        string      `json:"path"`
	Branch      string      `json:"branch"`
	Upstream    string      `json:"upstream"`
	Remote      string      `json:"remote"`
	Detached    bool        `json:"detached"`
	HasUpstream bool        `json:"hasUpstream"`
	Ahead       int         `json:"ahead"`
	Behind      int         `json:"behind"`
	Staged      int         `json:"staged"`
	Unstaged    int         `json:"unstaged"`
	Untracked   int         `json:"untracked"`
	Conflicts   int         `json:"conflicts"`
	Dirty       bool        `json:"dirty"`
	State       string      `json:"state"` // synced|ahead|behind|diverged|no-upstream|detached
	LastCommit  *CommitInfo `json:"lastCommit,omitempty"`
	LastFetch   int64       `json:"lastFetch"` // unix seconds, 0 if never
	Error       string      `json:"error,omitempty"`
}

// OpResult is the outcome of a git operation (fetch/pull/push/commit).
type OpResult struct {
	Repo   string `json:"repo"`
	Action string `json:"action"`
	OK     bool   `json:"ok"`
	Output string `json:"output"`
}

// runGit runs a git command inside dir and returns combined output.
// GIT_TERMINAL_PROMPT=0 prevents the server from hanging on a credential
// prompt; cached credential helpers (e.g. osxkeychain) still work.
func runGit(dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), ctx.Err()
	}
	return string(out), err
}

// runGitStdin is like runGit but pipes stdin into git (used for `git apply`).
func runGitStdin(dir string, timeout time.Duration, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
	)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), ctx.Err()
	}
	return string(out), err
}

// findRepos returns immediate subdirectories of root that contain a .git entry.
func findRepos(root string) []string {
	var names []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return names
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), ".git")); err == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// scanRepos collects status for every repo under root, in parallel.
func scanRepos(root string) []RepoInfo {
	names := findRepos(root)
	results := make([]RepoInfo, len(names))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, n := range names {
		wg.Add(1)
		go func(i int, n string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = repoStatus(root, n)
		}(i, n)
	}
	wg.Wait()
	return results
}

func repoStatus(root, name string) RepoInfo {
	dir := filepath.Join(root, name)
	info := RepoInfo{Name: name, Path: dir}

	out, err := runGit(dir, 10*time.Second, "status", "--porcelain=v2", "--branch")
	if err != nil {
		info.Error = strings.TrimSpace(out)
		if info.Error == "" {
			info.Error = err.Error()
		}
		return info
	}
	parseStatus(&info, out)

	if r, err := runGit(dir, 5*time.Second, "remote", "get-url", "origin"); err == nil {
		info.Remote = strings.TrimSpace(r)
	}
	if c, err := runGit(dir, 5*time.Second, "log", "-1", "--format=%H%x1f%h%x1f%s%x1f%ct%x1f%an"); err == nil {
		if cm := parseCommitLine(strings.TrimRight(c, "\n")); cm != nil {
			info.LastCommit = cm
		}
	}
	if st, err := os.Stat(filepath.Join(dir, ".git", "FETCH_HEAD")); err == nil {
		info.LastFetch = st.ModTime().Unix()
	}

	info.deriveState()
	return info
}

func parseStatus(info *RepoInfo, out string) {
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			switch fields[1] {
			case "branch.head":
				if len(fields) >= 3 {
					if fields[2] == "(detached)" {
						info.Detached = true
					} else {
						info.Branch = fields[2]
					}
				}
			case "branch.upstream":
				if len(fields) >= 3 {
					info.Upstream = fields[2]
					info.HasUpstream = true
				}
			case "branch.ab":
				for _, f := range fields[2:] {
					switch {
					case strings.HasPrefix(f, "+"):
						info.Ahead = atoi(f[1:])
					case strings.HasPrefix(f, "-"):
						info.Behind = atoi(f[1:])
					}
				}
			}
			continue
		}
		switch line[0] {
		case '1', '2': // ordinary / renamed change
			if len(line) >= 4 {
				x, y := line[2], line[3]
				if x != '.' {
					info.Staged++
				}
				if y != '.' {
					info.Unstaged++
				}
			}
		case 'u': // unmerged
			info.Conflicts++
		case '?': // untracked
			info.Untracked++
		}
	}
	info.Dirty = info.Staged+info.Unstaged+info.Untracked+info.Conflicts > 0
}

func (info *RepoInfo) deriveState() {
	switch {
	case info.Detached:
		info.State = "detached"
	case !info.HasUpstream:
		info.State = "no-upstream"
	case info.Ahead > 0 && info.Behind > 0:
		info.State = "diverged"
	case info.Ahead > 0:
		info.State = "ahead"
	case info.Behind > 0:
		info.State = "behind"
	default:
		info.State = "synced"
	}
}

// repoFiles lists changed files for the detail view.
func repoFiles(root, name string) []FileChange {
	dir := filepath.Join(root, name)
	out, err := runGit(dir, 10*time.Second, "-c", "core.quotepath=false", "status", "--short")
	if err != nil {
		return nil
	}
	var files []FileChange
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		l := sc.Text()
		if len(l) < 4 {
			continue
		}
		x := l[0]
		path := strings.TrimSpace(l[3:])
		files = append(files, FileChange{
			Path:   path,
			Code:   l[:2],
			Staged: x != ' ' && x != '?',
		})
	}
	return files
}

// recentLog returns the most recent n commits.
func recentLog(root, name string, n int) []CommitInfo {
	dir := filepath.Join(root, name)
	out, err := runGit(dir, 10*time.Second, "log", "-n", strconv.Itoa(n),
		"--format=%H%x1f%h%x1f%s%x1f%ct%x1f%an")
	if err != nil {
		return nil
	}
	var logs []CommitInfo
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		if cm := parseCommitLine(sc.Text()); cm != nil {
			logs = append(logs, *cm)
		}
	}
	return logs
}

func parseCommitLine(line string) *CommitInfo {
	parts := strings.Split(line, "\x1f")
	if len(parts) != 5 {
		return nil
	}
	t, _ := strconv.ParseInt(parts[3], 10, 64)
	return &CommitInfo{
		Hash:    parts[0],
		Short:   parts[1],
		Subject: parts[2],
		Time:    t,
		Author:  parts[4],
	}
}

// ---- operations ----

func fetchRepo(root, name string) OpResult {
	out, err := runGit(filepath.Join(root, name), 120*time.Second, "fetch", "--all", "--prune")
	return mkResult(name, "fetch", out, err)
}

// pullRepo pulls using the given mode: "ff" (default, safe), "rebase", "merge".
func pullRepo(root, name, mode string) OpResult {
	dir := filepath.Join(root, name)
	var args []string
	switch mode {
	case "rebase":
		args = []string{"pull", "--rebase"}
	case "merge":
		args = []string{"pull", "--no-rebase"}
	default:
		args = []string{"pull", "--ff-only"}
	}
	out, err := runGit(dir, 180*time.Second, args...)
	return mkResult(name, "pull", out, err)
}

// pushRepo pushes the current branch. If no upstream is set it creates one.
// force uses --force-with-lease (safe force).
func pushRepo(root, name string, force bool) OpResult {
	dir := filepath.Join(root, name)
	info := repoStatus(root, name)

	args := []string{"push"}
	if force {
		args = append(args, "--force-with-lease")
	}
	if !info.HasUpstream && info.Branch != "" {
		args = append(args, "-u", "origin", info.Branch)
	}
	out, err := runGit(dir, 180*time.Second, args...)
	return mkResult(name, "push", out, err)
}

func stageRepo(root, name string) OpResult {
	out, err := runGit(filepath.Join(root, name), 30*time.Second, "add", "-A")
	return mkResult(name, "stage", out, err)
}

// commitRepo stages everything, commits with msg, and optionally pushes.
func commitRepo(root, name, msg string, push bool) OpResult {
	dir := filepath.Join(root, name)
	if out, err := runGit(dir, 30*time.Second, "add", "-A"); err != nil {
		return mkResult(name, "commit", out, err)
	}
	out, err := runGit(dir, 30*time.Second, "commit", "-m", msg)
	res := mkResult(name, "commit", out, err)
	if res.OK && push {
		p := pushRepo(root, name, false)
		res.Output = strings.TrimSpace(res.Output + "\n\n" + p.Output)
		res.OK = p.OK
	}
	return res
}

func mkResult(name, action, out string, err error) OpResult {
	r := OpResult{Repo: name, Action: action, Output: strings.TrimSpace(out), OK: err == nil}
	if err != nil && r.Output == "" {
		r.Output = err.Error()
	}
	return r
}

// batchOp runs fn over names concurrently (network bound), bounded pool.
func batchOp(root string, names []string, fn func(root, name string) OpResult) []OpResult {
	results := make([]OpResult, len(names))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, n := range names {
		wg.Add(1)
		go func(i int, n string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = fn(root, n)
		}(i, n)
	}
	wg.Wait()
	return results
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
