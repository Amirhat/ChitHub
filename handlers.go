package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type App struct {
	mu  sync.RWMutex
	cfg Config
	hub *hub

	// Lifecycle (app mode only): the process exits shortly after the last UI
	// window disconnects, so it never lingers holding the port. See lifecycle.go.
	srv         *http.Server
	appMode     atomic.Bool // window-tied shutdown is armed
	sawActivity atomic.Bool // first HTTP request seen (a window actually opened)
	closing     atomic.Bool // an explicit window-closed beacon was received
	quitMu      sync.Mutex
	quitTimer   *time.Timer
	firstGrace  time.Duration // backstop if no window ever connects
	dropGrace   time.Duration // SSE dropped (blip/sleep) — tolerate slow reconnects
	closeGrace  time.Duration // explicit window-closed beacon — quit promptly
	onQuit      func(reason string) // overridable in tests; nil ⇒ real shutdown + exit
}

func (a *App) root() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.Active
}

func (a *App) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/config", a.handleGetConfig)
	mux.HandleFunc("POST /api/collections", a.handleCollections)
	mux.HandleFunc("POST /api/pick-folder", a.handlePickFolder)
	mux.HandleFunc("GET /api/review", a.handleReview)
	mux.HandleFunc("GET /api/repos", a.handleRepos)
	mux.HandleFunc("GET /api/repo/{name}", a.handleRepoDetail)
	mux.HandleFunc("GET /api/repo/{name}/diff", a.handleDiff)
	mux.HandleFunc("GET /api/repo/{name}/branches", a.handleBranches)
	mux.HandleFunc("POST /api/repo/{name}/fetch", a.handleFetch)
	mux.HandleFunc("POST /api/repo/{name}/pull", a.handlePull)
	mux.HandleFunc("POST /api/repo/{name}/push", a.handlePush)
	mux.HandleFunc("POST /api/repo/{name}/stage", a.handleStage)
	mux.HandleFunc("POST /api/repo/{name}/commit", a.handleCommit)
	mux.HandleFunc("POST /api/repo/{name}/amend", a.handleAmend)
	mux.HandleFunc("POST /api/repo/{name}/undo", a.handleUndo)
	mux.HandleFunc("GET /api/repo/{name}/show", a.handleShow)
	mux.HandleFunc("POST /api/repo/{name}/checkout", a.handleCheckout)
	mux.HandleFunc("POST /api/repo/{name}/discard", a.handleDiscard)
	mux.HandleFunc("POST /api/repo/{name}/discard-patch", a.handleDiscardPatch)
	mux.HandleFunc("POST /api/repo/{name}/stash", a.handleStash)
	mux.HandleFunc("POST /api/repo/{name}/reveal", a.handleReveal)
	mux.HandleFunc("POST /api/clone", a.handleClone)
	mux.HandleFunc("POST /api/batch", a.handleBatch)

	// live updates + preferences
	mux.HandleFunc("GET /api/events", a.handleEvents)
	mux.HandleFunc("POST /api/window-closed", a.handleWindowClosed)
	mux.HandleFunc("GET /api/settings", a.handleGetSettings)
	mux.HandleFunc("POST /api/settings", a.handleSetSettings)
	mux.HandleFunc("GET /api/update-check", a.handleUpdateCheck)

	// branch operations
	mux.HandleFunc("POST /api/repo/{name}/publish", a.handlePublish)
	mux.HandleFunc("POST /api/repo/{name}/branch-delete", a.handleBranchDelete)
	mux.HandleFunc("POST /api/repo/{name}/branch-rename", a.handleBranchRename)
	mux.HandleFunc("POST /api/repo/{name}/merge", a.handleMerge)
	mux.HandleFunc("POST /api/repo/{name}/rebase", a.handleRebase)
	mux.HandleFunc("POST /api/repo/{name}/sync", a.handleSync)

	// history
	mux.HandleFunc("GET /api/repo/{name}/history", a.handleHistory)
	mux.HandleFunc("POST /api/repo/{name}/revert", a.handleRevert)
	mux.HandleFunc("POST /api/repo/{name}/cherry-pick", a.handleCherryPick)
	mux.HandleFunc("POST /api/repo/{name}/reset", a.handleReset)
	mux.HandleFunc("GET /api/repo/{name}/blame", a.handleBlame)

	// tags
	mux.HandleFunc("GET /api/repo/{name}/tags", a.handleTags)
	mux.HandleFunc("POST /api/repo/{name}/tag", a.handleTag)

	// conflicts / sequencer
	mux.HandleFunc("GET /api/repo/{name}/conflicts", a.handleConflicts)
	mux.HandleFunc("GET /api/repo/{name}/conflict", a.handleConflict)
	mux.HandleFunc("POST /api/repo/{name}/resolve", a.handleResolve)
	mux.HandleFunc("POST /api/repo/{name}/sequencer", a.handleSequencer)

	// integrations
	mux.HandleFunc("POST /api/repo/{name}/open", a.handleOpen)
	mux.HandleFunc("GET /api/repo/{name}/ci", a.handleCI)
	mux.HandleFunc("POST /api/repo/{name}/pr", a.handlePR)

	// multi-repo
	mux.HandleFunc("POST /api/bulk/commit", a.handleBulkCommit)
	mux.HandleFunc("POST /api/bulk/checkout", a.handleBulkCheckout)
	mux.HandleFunc("GET /api/search", a.handleSearch)
	mux.HandleFunc("GET /api/snapshot", a.handleSnapshot)
	mux.HandleFunc("POST /api/snapshot/restore", a.handleSnapshotRestore)
	mux.HandleFunc("POST /api/run", a.handleRun)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *App) validRepo(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, n := range findRepos(a.root()) {
		if n == name {
			return true
		}
	}
	return false
}

// CollectionInfo describes one tracked parent folder for the UI switcher.
type CollectionInfo struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Exists    bool   `json:"exists"`
	RepoCount int    `json:"repoCount"`
}

func collectionInfos(paths []string) []CollectionInfo {
	out := make([]CollectionInfo, 0, len(paths))
	for _, p := range paths {
		ci := CollectionInfo{Path: p, Name: filepath.Base(p)}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			ci.Exists = true
			ci.RepoCount = len(findRepos(p))
		}
		out = append(out, ci)
	}
	return out
}

func (a *App) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"collections": collectionInfos(cfg.Collections),
		"active":      cfg.Active,
		"port":        cfg.Port,
	})
}

// handleCollections adds, removes, or switches the active collection folder.
func (a *App) handleCollections(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"` // add | remove | switch
		Path   string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	p := strings.TrimSpace(body.Path)
	if p == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}

	if body.Action == "add" || body.Action == "" {
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "folder does not exist: " + p})
			return
		}
	}

	a.mu.Lock()
	switch body.Action {
	case "remove":
		a.cfg.RemoveCollection(p)
	case "switch":
		a.cfg.SetActive(p)
	default:
		a.cfg.AddCollection(p)
	}
	cfg := a.cfg
	a.mu.Unlock()
	saveConfig(cfg)

	writeJSON(w, http.StatusOK, map[string]any{
		"collections": collectionInfos(cfg.Collections),
		"active":      cfg.Active,
	})
}

// handleReview returns the ordered work queues for the guided review flows.
func (a *App) handleReview(w http.ResponseWriter, r *http.Request) {
	repos := scanRepos(a.root())
	var commit, pull []string
	for _, rp := range repos {
		if rp.Error != "" {
			continue
		}
		if rp.Dirty || rp.Ahead > 0 {
			commit = append(commit, rp.Name)
		}
		if rp.Behind > 0 {
			pull = append(pull, rp.Name)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"commit": commit, // repos needing commit/push attention
		"pull":   pull,   // repos that are behind
	})
}

func (a *App) handleRepos(w http.ResponseWriter, r *http.Request) {
	root := a.root()
	repos := scanRepos(root)
	writeJSON(w, http.StatusOK, map[string]any{
		"root":  root,
		"repos": repos,
	})
}

func (a *App) handleRepoDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	root := a.root()
	writeJSON(w, http.StatusOK, map[string]any{
		"info":     repoStatus(root, name),
		"files":    repoFiles(root, name),
		"log":      recentLog(root, name, 15),
		"incoming": incomingLog(root, name),
		"branches": repoBranches(root, name),
		"stashes":  stashList(root, name),
	})
}

func (a *App) handleDiff(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	context := 3
	if c := r.URL.Query().Get("context"); c != "" {
		if n, err := strconv.Atoi(c); err == nil {
			context = n
		}
	}
	writeJSON(w, http.StatusOK, fileDiffN(a.root(), name, path, context))
}

func (a *App) handleBranches(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	writeJSON(w, http.StatusOK, repoBranches(a.root(), name))
}

func (a *App) handleAmend(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Message string       `json:"message"`
		Push    bool         `json:"push"`
		Files   []CommitFile `json:"files"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusOK, amendCommit(a.root(), name, body.Message, body.Files, body.Push))
}

func (a *App) handleUndo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	writeJSON(w, http.StatusOK, undoLastCommit(a.root(), name))
}

func (a *App) handleShow(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	hash := r.URL.Query().Get("hash")
	writeJSON(w, http.StatusOK, map[string]any{"files": commitShow(a.root(), name, hash)})
}

func (a *App) handleCheckout(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Branch     string `json:"branch"`
		Create     bool   `json:"create"`
		StartPoint string `json:"startPoint"`
		Stash      bool   `json:"stash"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Branch) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "branch is required"})
		return
	}
	writeJSON(w, http.StatusOK, checkoutBranch(a.root(), name, strings.TrimSpace(body.Branch), body.Create, body.StartPoint, body.Stash))
}

func (a *App) handlePickFolder(w http.ResponseWriter, r *http.Request) {
	path, err := pickFolder()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

func (a *App) handleDiscard(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Paths   []string `json:"paths"`
		ToStash bool     `json:"toStash"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.ToStash {
		writeJSON(w, http.StatusOK, stashDiscard(a.root(), name, body.Paths))
		return
	}
	writeJSON(w, http.StatusOK, discardChanges(a.root(), name, body.Paths))
}

func (a *App) handleDiscardPatch(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Patch string `json:"patch"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusOK, discardPatch(a.root(), name, body.Patch))
}

func (a *App) handleStash(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Action  string `json:"action"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusOK, stashOp(a.root(), name, body.Action, body.Message))
}

func (a *App) handleReveal(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	err := revealInFinder(filepath.Join(a.root(), name))
	writeJSON(w, http.StatusOK, map[string]any{"ok": err == nil})
}

func (a *App) handleClone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(body.URL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if strings.ContainsAny(name, "/\\") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid folder name"})
		return
	}
	writeJSON(w, http.StatusOK, cloneRepo(a.root(), strings.TrimSpace(body.URL), name))
}

func (a *App) handleFetch(w http.ResponseWriter, r *http.Request) {
	a.runSingle(w, r, func(root, name string) OpResult { return fetchRepo(root, name) })
}

func (a *App) handlePull(w http.ResponseWriter, r *http.Request) {
	mode := pullMode(r)
	a.runSingle(w, r, func(root, name string) OpResult { return pullRepo(root, name, mode) })
}

func (a *App) handlePush(w http.ResponseWriter, r *http.Request) {
	force := boolField(r, "force")
	a.runSingle(w, r, func(root, name string) OpResult { return pushRepo(root, name, force) })
}

func (a *App) handleStage(w http.ResponseWriter, r *http.Request) {
	a.runSingle(w, r, func(root, name string) OpResult { return stageRepo(root, name) })
}

func (a *App) handleCommit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message string       `json:"message"`
		Push    bool         `json:"push"`
		Files   []CommitFile `json:"files"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	msg := strings.TrimSpace(body.Message)
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	if msg == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commit message is required"})
		return
	}
	var res OpResult
	if len(body.Files) == 0 {
		// No explicit selection: stage and commit everything (legacy behavior).
		res = commitRepo(a.root(), name, msg, body.Push)
	} else {
		res = commitSelective(a.root(), name, msg, body.Files, body.Push)
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *App) runSingle(w http.ResponseWriter, r *http.Request, fn func(root, name string) OpResult) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	writeJSON(w, http.StatusOK, fn(a.root(), name))
}

func (a *App) handleBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string   `json:"action"` // fetch|pull|push
		Mode   string   `json:"mode"`   // for pull
		Force  bool     `json:"force"`  // for push
		Repos  []string `json:"repos"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	root := a.root()
	var valid []string
	for _, n := range body.Repos {
		if a.validRepo(n) {
			valid = append(valid, n)
		}
	}

	var fn func(root, name string) OpResult
	switch body.Action {
	case "fetch":
		fn = func(root, name string) OpResult { return fetchRepo(root, name) }
	case "pull":
		mode := body.Mode
		fn = func(root, name string) OpResult { return pullRepo(root, name, mode) }
	case "push":
		force := body.Force
		fn = func(root, name string) OpResult { return pushRepo(root, name, force) }
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"results": batchOp(root, valid, fn),
	})
}

func pullMode(r *http.Request) string {
	var body struct {
		Mode string `json:"mode"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch body.Mode {
	case "rebase", "merge", "ff":
		return body.Mode
	default:
		return "ff"
	}
}

func boolField(r *http.Request, field string) bool {
	m := map[string]bool{}
	_ = json.NewDecoder(r.Body).Decode(&m)
	return m[field]
}
