package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ---- settings ----

func (a *App) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	s := a.cfg.Settings
	a.mu.RUnlock()
	writeJSON(w, http.StatusOK, s)
}

func (a *App) handleSetSettings(w http.ResponseWriter, r *http.Request) {
	var s Settings
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	s.normalize()
	a.mu.Lock()
	a.cfg.Settings = s
	cfg := a.cfg
	a.mu.Unlock()
	saveConfig(cfg)
	writeJSON(w, http.StatusOK, s)
}

// ---- update check ----

func (a *App) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	tag, url, notes := latestRelease()
	writeJSON(w, http.StatusOK, map[string]any{
		"current":   version,
		"latest":    tag,
		"url":       url,
		"notes":     notes,
		"hasUpdate": tag != "" && version != "dev" && normalizeTag(tag) != normalizeTag(version),
	})
}

const releaseSlug = "Amirhat/ChitHub"

func latestRelease() (tag, url, notes string) {
	client := &http.Client{Timeout: 6 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/"+releaseSlug+"/releases/latest", nil)
	if err != nil {
		return "", "", ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", ""
	}
	var data struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", ""
	}
	return data.TagName, data.HTMLURL, data.Body
}

func normalizeTag(s string) string { return strings.TrimPrefix(strings.TrimSpace(s), "v") }

// ---- branch ops ----

func (a *App) handlePublish(w http.ResponseWriter, r *http.Request) {
	a.runSingle(w, r, func(root, name string) OpResult { return publishBranch(root, name) })
}

func (a *App) handleBranchDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Branch string `json:"branch"`
		Remote bool   `json:"remote"`
		Force  bool   `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Branch) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "branch is required"})
		return
	}
	writeJSON(w, http.StatusOK, deleteBranch(a.root(), name, body.Branch, body.Remote, body.Force))
}

func (a *App) handleBranchRename(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.To) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new name is required"})
		return
	}
	writeJSON(w, http.StatusOK, renameBranch(a.root(), name, body.From, body.To))
}

func (a *App) handleMerge(w http.ResponseWriter, r *http.Request) {
	a.opWithBranch(w, r, func(root, name, branch string) OpResult { return mergeBranch(root, name, branch) })
}

func (a *App) handleRebase(w http.ResponseWriter, r *http.Request) {
	a.opWithBranch(w, r, func(root, name, branch string) OpResult { return rebaseOnto(root, name, branch) })
}

func (a *App) opWithBranch(w http.ResponseWriter, r *http.Request, fn func(root, name, branch string) OpResult) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Branch string `json:"branch"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Branch) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "branch is required"})
		return
	}
	writeJSON(w, http.StatusOK, fn(a.root(), name, strings.TrimSpace(body.Branch)))
}

func (a *App) handleSync(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusOK, syncRepo(a.root(), name, body.Mode))
}

// ---- history ----

func (a *App) handleHistory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	q := r.URL.Query()
	skip, _ := strconv.Atoi(q.Get("skip"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	writeJSON(w, http.StatusOK, map[string]any{
		"commits": fullHistory(a.root(), name, skip, limit, q.Get("q"), q.Get("path")),
	})
}

func (a *App) handleRevert(w http.ResponseWriter, r *http.Request) {
	a.opWithHash(w, r, func(root, name, hash string) OpResult { return revertCommit(root, name, hash) })
}

func (a *App) handleCherryPick(w http.ResponseWriter, r *http.Request) {
	a.opWithHash(w, r, func(root, name, hash string) OpResult { return cherryPick(root, name, hash) })
}

func (a *App) handleReset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Hash   string `json:"hash"`
		Mode   string `json:"mode"`
		Backup bool   `json:"backup"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusOK, resetTo(a.root(), name, strings.TrimSpace(body.Hash), body.Mode, body.Backup))
}

func (a *App) opWithHash(w http.ResponseWriter, r *http.Request, fn func(root, name, hash string) OpResult) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Hash string `json:"hash"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Hash) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hash is required"})
		return
	}
	writeJSON(w, http.StatusOK, fn(a.root(), name, strings.TrimSpace(body.Hash)))
}

func (a *App) handleBlame(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, map[string]any{"lines": blameFile(a.root(), name, path)})
}

// ---- tags ----

func (a *App) handleTags(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": listTags(a.root(), name)})
}

func (a *App) handleTag(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Action  string `json:"action"`
		Name    string `json:"name"`
		Ref     string `json:"ref"`
		Message string `json:"message"`
		Push    bool   `json:"push"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tag name is required"})
		return
	}
	writeJSON(w, http.StatusOK, tagOp(a.root(), name, body.Action, body.Name, body.Ref, body.Message, body.Push))
}

// ---- conflicts ----

func (a *App) handleConflicts(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	writeJSON(w, http.StatusOK, conflictState(a.root(), name))
}

func (a *App) handleConflict(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, conflictFile(a.root(), name, path))
}

func (a *App) handleResolve(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Path    string `json:"path"`
		Side    string `json:"side"`
		Content string `json:"content"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	writeJSON(w, http.StatusOK, resolveConflict(a.root(), name, body.Path, body.Side, body.Content))
}

func (a *App) handleSequencer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Op     string `json:"op"`
		Action string `json:"action"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusOK, sequencerOp(a.root(), name, body.Op, body.Action))
}

// ---- integrations ----

func (a *App) handleOpen(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Target string `json:"target"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusOK, openTarget(a.root(), name, body.Target))
}

func (a *App) handleCI(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	writeJSON(w, http.StatusOK, ciStatus(a.root(), name))
}

func (a *App) handlePR(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.validRepo(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown repo"})
		return
	}
	var body struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusOK, createPR(a.root(), name, body.Title, body.Body))
}

// ---- multi-repo ----

func (a *App) validRepos(in []string) []string {
	var out []string
	for _, n := range in {
		if a.validRepo(n) {
			out = append(out, n)
		}
	}
	return out
}

func (a *App) handleBulkCommit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repos   []string `json:"repos"`
		Message string   `json:"message"`
		Push    bool     `json:"push"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Message) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commit message is required"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": bulkCommit(a.root(), a.validRepos(body.Repos), strings.TrimSpace(body.Message), body.Push),
	})
}

func (a *App) handleBulkCheckout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repos  []string `json:"repos"`
		Branch string   `json:"branch"`
		Create bool     `json:"create"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Branch) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "branch is required"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": bulkCheckout(a.root(), a.validRepos(body.Repos), strings.TrimSpace(body.Branch), body.Create),
	})
}

func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	writeJSON(w, http.StatusOK, map[string]any{
		"hits": crossRepoSearch(a.root(), q, findRepos(a.root())),
	})
}

func (a *App) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"entries": workspaceSnapshot(a.root())})
}

func (a *App) handleSnapshotRestore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Entries []SnapEntry `json:"entries"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusOK, map[string]any{"results": restoreSnapshot(a.root(), body.Entries)})
}

func (a *App) handleRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repos []string `json:"repos"`
		Cmd   string   `json:"cmd"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Cmd) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": runInRepos(a.root(), a.validRepos(body.Repos), body.Cmd),
	})
}
