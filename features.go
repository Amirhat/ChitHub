package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ===================== Branches =====================

func publishBranch(root, name string) OpResult {
	dir := filepath.Join(root, name)
	info := repoStatus(root, name)
	if info.Branch == "" {
		return OpResult{Repo: name, Action: "publish", OK: false, Output: "Not on a branch (detached HEAD)."}
	}
	out, err := runGit(dir, 180*time.Second, "push", "-u", "origin", info.Branch)
	return mkResult(name, "publish", out, err)
}

func deleteBranch(root, name, branch string, remote, force bool) OpResult {
	dir := filepath.Join(root, name)
	if remote {
		out, err := runGit(dir, 120*time.Second, "push", "origin", "--delete", branch)
		return mkResult(name, "delete-remote", out, err)
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	out, err := runGit(dir, 15*time.Second, "branch", flag, branch)
	return mkResult(name, "delete-branch", out, err)
}

func renameBranch(root, name, from, to string) OpResult {
	dir := filepath.Join(root, name)
	args := []string{"branch", "-m"}
	if strings.TrimSpace(from) != "" {
		args = append(args, from)
	}
	args = append(args, to)
	out, err := runGit(dir, 15*time.Second, args...)
	return mkResult(name, "rename-branch", out, err)
}

func mergeBranch(root, name, branch string) OpResult {
	out, err := runGit(filepath.Join(root, name), 120*time.Second, "merge", "--no-edit", branch)
	return mkResult(name, "merge", out, err)
}

func rebaseOnto(root, name, branch string) OpResult {
	out, err := runGit(filepath.Join(root, name), 120*time.Second, "rebase", branch)
	return mkResult(name, "rebase", out, err)
}

// ===================== History / graph =====================

type HistoryCommit struct {
	Hash    string   `json:"hash"`
	Short   string   `json:"short"`
	Subject string   `json:"subject"`
	Time    int64    `json:"time"`
	Author  string   `json:"author"`
	Parents []string `json:"parents"`
	Refs    []string `json:"refs"`
}

func fullHistory(root, name string, skip, limit int, query, path string) []HistoryCommit {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if skip < 0 {
		skip = 0
	}
	args := []string{"log",
		"--skip=" + strconv.Itoa(skip), "-n", strconv.Itoa(limit),
		"--date-order",
		"--format=%H%x1f%h%x1f%s%x1f%ct%x1f%an%x1f%P%x1f%D"}
	if q := strings.TrimSpace(query); q != "" {
		// Match the term in the commit message (case-insensitive).
		args = append(args, "--regexp-ignore-case", "--grep="+q)
	}
	if p := strings.TrimSpace(path); p != "" {
		args = append(args, "--", p)
	}
	out, err := runGit(filepath.Join(root, name), 20*time.Second, args...)
	if err != nil {
		return nil
	}
	var hist []HistoryCommit
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		parts := strings.Split(line, "\x1f")
		if len(parts) < 7 {
			continue
		}
		t, _ := strconv.ParseInt(parts[3], 10, 64)
		hc := HistoryCommit{
			Hash: parts[0], Short: parts[1], Subject: parts[2], Time: t, Author: parts[4],
		}
		if parts[5] != "" {
			hc.Parents = strings.Fields(parts[5])
		}
		if parts[6] != "" {
			for _, r := range strings.Split(parts[6], ",") {
				r = strings.TrimSpace(r)
				r = strings.TrimPrefix(r, "HEAD -> ")
				if r != "" {
					hc.Refs = append(hc.Refs, r)
				}
			}
		}
		hist = append(hist, hc)
	}
	return hist
}

func revertCommit(root, name, hash string) OpResult {
	if !isHexish(hash) {
		return OpResult{Repo: name, Action: "revert", OK: false, Output: "invalid commit"}
	}
	out, err := runGit(filepath.Join(root, name), 30*time.Second, "revert", "--no-edit", hash)
	return mkResult(name, "revert", out, err)
}

func cherryPick(root, name, hash string) OpResult {
	if !isHexish(hash) {
		return OpResult{Repo: name, Action: "cherry-pick", OK: false, Output: "invalid commit"}
	}
	out, err := runGit(filepath.Join(root, name), 30*time.Second, "cherry-pick", hash)
	return mkResult(name, "cherry-pick", out, err)
}

func resetTo(root, name, hash, mode string) OpResult {
	if !isHexish(hash) {
		return OpResult{Repo: name, Action: "reset", OK: false, Output: "invalid commit"}
	}
	flag := "--mixed"
	switch mode {
	case "soft":
		flag = "--soft"
	case "hard":
		flag = "--hard"
	}
	out, err := runGit(filepath.Join(root, name), 30*time.Second, "reset", flag, hash)
	r := mkResult(name, "reset", out, err)
	if r.OK && r.Output == "" {
		r.Output = "Reset " + mode + " to " + hash[:min(7, len(hash))] + "."
	}
	return r
}

type BlameLine struct {
	Short   string `json:"short"`
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

func blameFile(root, name, path string) []BlameLine {
	out, err := runGit(filepath.Join(root, name), 30*time.Second, "blame", "--porcelain", "--", path)
	if err != nil {
		return nil
	}
	var lines []BlameLine
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	commitMeta := map[string][2]string{} // sha -> [author, time]
	var curSha, curAuthor, curTime string
	var curLineNo int
	headerRe := regexp.MustCompile(`^[0-9a-f]{40} \d+ (\d+)`)
	for sc.Scan() {
		l := sc.Text()
		if m := headerRe.FindStringSubmatch(l); m != nil {
			curSha = l[:40]
			curLineNo, _ = strconv.Atoi(m[1])
			if meta, ok := commitMeta[curSha]; ok {
				curAuthor, curTime = meta[0], meta[1]
			}
			continue
		}
		if strings.HasPrefix(l, "author ") {
			curAuthor = strings.TrimPrefix(l, "author ")
		} else if strings.HasPrefix(l, "author-time ") {
			curTime = strings.TrimPrefix(l, "author-time ")
		} else if strings.HasPrefix(l, "\t") {
			commitMeta[curSha] = [2]string{curAuthor, curTime}
			t, _ := strconv.ParseInt(curTime, 10, 64)
			short := curSha
			if len(short) > 8 {
				short = short[:8]
			}
			lines = append(lines, BlameLine{
				Short: short, Author: curAuthor, Time: t, Line: curLineNo, Content: l[1:],
			})
		}
	}
	return lines
}

// ===================== Tags =====================

type TagInfo struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Short   string `json:"short"`
}

func listTags(root, name string) []TagInfo {
	out, err := runGit(filepath.Join(root, name), 8*time.Second,
		"for-each-ref", "--sort=-creatordate", "--count=200",
		"--format=%(refname:short)%1f%(objectname:short)%1f%(contents:subject)", "refs/tags")
	if err != nil {
		return nil
	}
	var tags []TagInfo
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l == "" {
			continue
		}
		p := strings.Split(l, "\x1f")
		if len(p) < 2 {
			continue
		}
		ti := TagInfo{Name: p[0], Short: p[1]}
		if len(p) >= 3 {
			ti.Subject = p[2]
		}
		tags = append(tags, ti)
	}
	return tags
}

func tagOp(root, name, action, tag, ref, message string, push bool) OpResult {
	dir := filepath.Join(root, name)
	switch action {
	case "delete":
		out, err := runGit(dir, 15*time.Second, "tag", "-d", tag)
		res := mkResult(name, "tag-delete", out, err)
		if res.OK && push {
			o, e := runGit(dir, 60*time.Second, "push", "origin", ":refs/tags/"+tag)
			res.Output = strings.TrimSpace(res.Output + "\n" + o)
			res.OK = e == nil
		}
		return res
	default: // create
		args := []string{"tag"}
		if strings.TrimSpace(message) != "" {
			args = append(args, "-a", tag, "-m", message)
		} else {
			args = append(args, tag)
		}
		if strings.TrimSpace(ref) != "" {
			args = append(args, ref)
		}
		out, err := runGit(dir, 15*time.Second, args...)
		res := mkResult(name, "tag", out, err)
		if res.OK && push {
			o, e := runGit(dir, 60*time.Second, "push", "origin", tag)
			res.Output = strings.TrimSpace(res.Output + "\n" + o)
			res.OK = e == nil
		}
		if res.OK && res.Output == "" {
			res.Output = "Created tag " + tag + "."
		}
		return res
	}
}

// ===================== Conflicts / sequencer =====================

type ConflictState struct {
	InProgress string   `json:"inProgress"` // "", "merge", "rebase", "cherry-pick", "revert"
	Files      []string `json:"files"`
}

func conflictState(root, name string) ConflictState {
	dir := filepath.Join(root, name)
	gitDir := filepath.Join(dir, ".git")
	var cs ConflictState
	switch {
	case fileExists(filepath.Join(gitDir, "MERGE_HEAD")):
		cs.InProgress = "merge"
	case fileExists(filepath.Join(gitDir, "rebase-merge")) || fileExists(filepath.Join(gitDir, "rebase-apply")):
		cs.InProgress = "rebase"
	case fileExists(filepath.Join(gitDir, "CHERRY_PICK_HEAD")):
		cs.InProgress = "cherry-pick"
	case fileExists(filepath.Join(gitDir, "REVERT_HEAD")):
		cs.InProgress = "revert"
	}
	out, err := runGit(dir, 10*time.Second, "diff", "--name-only", "--diff-filter=U")
	if err == nil {
		cs.Files = splitLines(out)
	}
	return cs
}

type ConflictFile struct {
	Path   string `json:"path"`
	Ours   string `json:"ours"`
	Theirs string `json:"theirs"`
	Merged string `json:"merged"` // working-tree content with conflict markers
}

func conflictFile(root, name, path string) ConflictFile {
	dir := filepath.Join(root, name)
	cf := ConflictFile{Path: path}
	if o, err := runGit(dir, 10*time.Second, "show", ":2:"+path); err == nil {
		cf.Ours = o
	}
	if t, err := runGit(dir, 10*time.Second, "show", ":3:"+path); err == nil {
		cf.Theirs = t
	}
	if b, err := os.ReadFile(filepath.Join(dir, path)); err == nil {
		cf.Merged = string(b)
	}
	return cf
}

func resolveConflict(root, name, path, side, content string) OpResult {
	dir := filepath.Join(root, name)
	switch side {
	case "ours":
		if o, e := runGit(dir, 15*time.Second, "checkout", "--ours", "--", path); e != nil {
			return mkResult(name, "resolve", o, e)
		}
	case "theirs":
		if o, e := runGit(dir, 15*time.Second, "checkout", "--theirs", "--", path); e != nil {
			return mkResult(name, "resolve", o, e)
		}
	case "content":
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
			return OpResult{Repo: name, Action: "resolve", OK: false, Output: err.Error()}
		}
	}
	out, err := runGit(dir, 15*time.Second, "add", "--", path)
	res := mkResult(name, "resolve", out, err)
	if res.OK && res.Output == "" {
		res.Output = "Marked " + path + " resolved."
	}
	return res
}

func sequencerOp(root, name, op, action string) OpResult {
	dir := filepath.Join(root, name)
	var cmd string
	switch op {
	case "merge", "rebase", "cherry-pick", "revert":
		cmd = op
	default:
		return OpResult{Repo: name, Action: "sequencer", OK: false, Output: "unknown operation"}
	}
	flag := "--continue"
	switch action {
	case "abort":
		flag = "--abort"
	case "skip":
		flag = "--skip"
	}
	args := []string{cmd, flag}
	if cmd == "merge" && action == "continue" {
		args = []string{"merge", "--continue"}
	}
	env := ""
	if action == "continue" {
		env = "GIT_EDITOR=true"
	}
	out, err := runGitEnv(dir, 60*time.Second, []string{env}, args...)
	return mkResult(name, op+"-"+action, out, err)
}

// ===================== Sync =====================

func syncRepo(root, name, pullMode string) OpResult {
	dir := filepath.Join(root, name)
	var b strings.Builder
	step := func(label, out string) { b.WriteString("• " + label + "\n" + strings.TrimSpace(out) + "\n\n") }

	fo, _ := runGit(dir, 120*time.Second, "fetch", "--all", "--prune")
	step("fetch", fo)

	info := repoStatus(root, name)
	if info.Error != "" {
		return OpResult{Repo: name, Action: "sync", OK: false, Output: strings.TrimSpace(b.String())}
	}

	if info.HasUpstream && info.Behind > 0 {
		var args []string
		switch pullMode {
		case "rebase":
			args = []string{"pull", "--rebase"}
		case "merge":
			args = []string{"pull", "--no-rebase"}
		default:
			args = []string{"pull", "--ff-only"}
		}
		po, perr := runGit(dir, 180*time.Second, args...)
		step("pull", po)
		if perr != nil {
			return OpResult{Repo: name, Action: "sync", OK: false, Output: strings.TrimSpace(b.String())}
		}
		info = repoStatus(root, name)
	}

	if !info.HasUpstream || info.Ahead > 0 {
		p := pushRepo(root, name, false)
		step("push", p.Output)
		if !p.OK {
			return OpResult{Repo: name, Action: "sync", OK: false, Output: strings.TrimSpace(b.String())}
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		out = "Already in sync."
	}
	return OpResult{Repo: name, Action: "sync", OK: true, Output: out}
}

// ===================== Integrations =====================

// webURL derives a browsable https URL for the repo's origin remote.
func webURL(root, name string) string {
	out, err := runGit(filepath.Join(root, name), 5*time.Second, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return remoteToWeb(strings.TrimSpace(out))
}

func remoteToWeb(remote string) string {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	if remote == "" {
		return ""
	}
	if strings.HasPrefix(remote, "http://") || strings.HasPrefix(remote, "https://") {
		return remote
	}
	// ssh://git@host[:port]/owner/repo  — strip the scheme, any user, and a port
	if strings.HasPrefix(remote, "ssh://") {
		remote = strings.TrimPrefix(remote, "ssh://")
		if at := strings.Index(remote, "@"); at >= 0 {
			remote = remote[at+1:]
		}
		if slash := strings.Index(remote, "/"); slash >= 0 {
			host := remote[:slash]
			if c := strings.Index(host, ":"); c >= 0 {
				host = host[:c] // drop the ssh port (e.g. :22, :2222)
			}
			return "https://" + host + remote[slash:]
		}
		return "https://" + remote
	}
	// scp-style git@host:owner/repo
	if at := strings.Index(remote, "@"); at >= 0 {
		remote = remote[at+1:]
	}
	if colon := strings.Index(remote, ":"); colon >= 0 {
		host := remote[:colon]
		path := remote[colon+1:]
		return "https://" + host + "/" + path
	}
	return ""
}

func openTarget(root, name, target string) OpResult {
	dir := filepath.Join(root, name)
	switch target {
	case "web":
		u := webURL(root, name)
		if u == "" {
			return OpResult{Repo: name, Action: "open", OK: false, Output: "No web URL for origin."}
		}
		return mkOpen(name, openURL(u))
	case "finder":
		return mkOpen(name, revealInFinder(dir))
	case "terminal":
		return mkOpen(name, openTerminal(dir))
	case "editor":
		return mkOpen(name, openEditor(dir))
	default:
		return OpResult{Repo: name, Action: "open", OK: false, Output: "unknown target"}
	}
}

func mkOpen(name string, err error) OpResult {
	if err != nil {
		return OpResult{Repo: name, Action: "open", OK: false, Output: err.Error()}
	}
	return OpResult{Repo: name, Action: "open", OK: true, Output: "Opened."}
}

// ciStatus queries the latest CI run for the current branch via the `gh` CLI.
// Returns an empty (non-error) status when gh is unavailable or the repo isn't
// on GitHub, so the UI can simply hide the indicator.
type CIStatus struct {
	Available  bool   `json:"available"`
	Status     string `json:"status"`     // queued|in_progress|completed
	Conclusion string `json:"conclusion"` // success|failure|cancelled|...
	URL        string `json:"url"`
	Title      string `json:"title"`
}

func ciStatus(root, name string) CIStatus {
	if _, err := exec.LookPath("gh"); err != nil {
		return CIStatus{}
	}
	dir := filepath.Join(root, name)
	cmd := exec.Command("gh", "run", "list", "-L", "1",
		"--json", "status,conclusion,url,displayTitle")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return CIStatus{}
	}
	// Minimal JSON parse without a struct slice import churn.
	s := string(out)
	get := func(key string) string {
		i := strings.Index(s, "\""+key+"\":")
		if i < 0 {
			return ""
		}
		rest := s[i+len(key)+3:]
		q1 := strings.Index(rest, "\"")
		if q1 < 0 {
			return ""
		}
		rest = rest[q1+1:]
		q2 := strings.Index(rest, "\"")
		if q2 < 0 {
			return ""
		}
		return rest[:q2]
	}
	cs := CIStatus{
		Available:  true,
		Status:     get("status"),
		Conclusion: get("conclusion"),
		URL:        get("url"),
		Title:      get("displayTitle"),
	}
	return cs
}

func createPR(root, name, title, body string) OpResult {
	dir := filepath.Join(root, name)
	if _, err := exec.LookPath("gh"); err == nil {
		args := []string{"pr", "create"}
		if strings.TrimSpace(title) != "" {
			args = append(args, "--title", title, "--body", body)
		} else {
			args = append(args, "--fill")
		}
		cmd := exec.Command("gh", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err == nil {
			return OpResult{Repo: name, Action: "pr", OK: true, Output: strings.TrimSpace(string(out))}
		}
		// fall through to compare URL on failure
	}
	info := repoStatus(root, name)
	web := webURL(root, name)
	if web == "" || info.Branch == "" {
		return OpResult{Repo: name, Action: "pr", OK: false, Output: "Could not create a PR (no gh CLI and no web remote)."}
	}
	u := web + "/compare/" + info.Branch + "?expand=1"
	if err := openURL(u); err != nil {
		return OpResult{Repo: name, Action: "pr", OK: false, Output: err.Error()}
	}
	return OpResult{Repo: name, Action: "pr", OK: true, Output: "Opened a compare page in your browser:\n" + u}
}

// ===================== Multi-repo =====================

func bulkCommit(root string, repos []string, msg string, push bool) []OpResult {
	return mapRepos(repos, 4, func(n string) OpResult {
		info := repoStatus(root, n)
		if !info.Dirty {
			return OpResult{Repo: n, Action: "commit", OK: true, Output: "Nothing to commit."}
		}
		return commitRepo(root, n, msg, push)
	})
}

func bulkCheckout(root string, repos []string, branch string, create bool) []OpResult {
	return mapRepos(repos, 6, func(n string) OpResult {
		return checkoutBranch(root, n, branch, create, "", false)
	})
}

type SearchHit struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func crossRepoSearch(root, query string, repos []string) []SearchHit {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	var mu sync.Mutex
	var hits []SearchHit
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, n := range repos {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out, _ := runGit(filepath.Join(root, n), 20*time.Second,
				"grep", "-n", "-I", "--no-color", "--max-depth", "30", "-e", query)
			local := []SearchHit{}
			sc := bufio.NewScanner(strings.NewReader(out))
			sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
			for sc.Scan() && len(local) < 100 {
				l := sc.Text()
				// path:line:text
				a := strings.SplitN(l, ":", 3)
				if len(a) < 3 {
					continue
				}
				ln, _ := strconv.Atoi(a[1])
				local = append(local, SearchHit{Repo: n, Path: a[0], Line: ln, Text: a[2]})
			}
			mu.Lock()
			hits = append(hits, local...)
			mu.Unlock()
		}(n)
	}
	wg.Wait()
	return hits
}

type SnapEntry struct {
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`
}

func workspaceSnapshot(root string) []SnapEntry {
	names := findRepos(root)
	out := make([]SnapEntry, len(names))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, n := range names {
		wg.Add(1)
		go func(i int, n string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			info := repoStatus(root, n)
			out[i] = SnapEntry{Repo: n, Branch: info.Branch, Detached: info.Detached}
		}(i, n)
	}
	wg.Wait()
	return out
}

func restoreSnapshot(root string, entries []SnapEntry) []OpResult {
	return mapRepos(snapNames(entries), 6, func(n string) OpResult {
		branch := ""
		for _, e := range entries {
			if e.Repo == n {
				branch = e.Branch
			}
		}
		if branch == "" {
			return OpResult{Repo: n, Action: "restore", OK: true, Output: "skipped (no branch)"}
		}
		return checkoutBranch(root, n, branch, false, "", false)
	})
}

func snapNames(e []SnapEntry) []string {
	out := make([]string, 0, len(e))
	for _, s := range e {
		out = append(out, s.Repo)
	}
	return out
}

func runInRepos(root string, repos []string, command string) []OpResult {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	return mapRepos(repos, 4, func(n string) OpResult {
		ctx := filepath.Join(root, n)
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = ctx
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		done := make(chan struct{})
		var out []byte
		var err error
		go func() { out, err = cmd.CombinedOutput(); close(done) }()
		select {
		case <-done:
		case <-time.After(120 * time.Second):
			_ = cmd.Process.Kill()
			return OpResult{Repo: n, Action: "run", OK: false, Output: "timed out"}
		}
		return OpResult{Repo: n, Action: "run", OK: err == nil, Output: strings.TrimSpace(string(out))}
	})
}

// ===================== helpers =====================

func mapRepos(names []string, conc int, fn func(string) OpResult) []OpResult {
	results := make([]OpResult, len(names))
	var wg sync.WaitGroup
	if conc < 1 {
		conc = 1
	}
	sem := make(chan struct{}, conc)
	for i, n := range names {
		wg.Add(1)
		go func(i int, n string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = fn(n)
		}(i, n)
	}
	wg.Wait()
	return results
}

// runGitEnv runs git with extra environment variables (used to neutralize the
// editor on `--continue` so the server never blocks on an interactive prompt).
func runGitEnv(dir string, timeout time.Duration, extraEnv []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C",
		"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true")
	for _, e := range extraEnv {
		if e != "" {
			cmd.Env = append(cmd.Env, e)
		}
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), ctx.Err()
	}
	return string(out), err
}

func goos() string { return runtime.GOOS }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func openURL(u string) error {
	switch goos() {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	default:
		return exec.Command("xdg-open", u).Start()
	}
}

func openTerminal(dir string) error {
	switch goos() {
	case "darwin":
		return exec.Command("open", "-a", "Terminal", dir).Start()
	case "windows":
		c := exec.Command("cmd", "/c", "start", "cmd")
		c.Dir = dir
		return c.Start()
	default:
		for _, term := range []string{"x-terminal-emulator", "gnome-terminal", "konsole", "xterm"} {
			if _, err := exec.LookPath(term); err == nil {
				c := exec.Command(term)
				c.Dir = dir
				return c.Start()
			}
		}
		return exec.Command("xdg-open", dir).Start()
	}
}

func openEditor(dir string) error {
	// Prefer common GUI editors that accept a folder argument.
	for _, ed := range []string{"cursor", "code", "subl", "zed", "idea", "webstorm", "goland"} {
		if _, err := exec.LookPath(ed); err == nil {
			return exec.Command(ed, dir).Start()
		}
	}
	if ed := os.Getenv("VISUAL"); ed != "" {
		return exec.Command(ed, dir).Start()
	}
	if ed := os.Getenv("EDITOR"); ed != "" {
		return exec.Command(ed, dir).Start()
	}
	if goos() == "darwin" {
		return exec.Command("open", dir).Start()
	}
	return exec.Command("xdg-open", dir).Start()
}
