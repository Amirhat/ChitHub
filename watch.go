package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// hub is a tiny SSE fan-out: connected browsers each get a channel, and the
// watcher broadcasts a "refresh" whenever the active collection changes on disk.
type hub struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
	// onCount, if set, is called (outside the lock) with the new client count
	// whenever a UI window connects or disconnects. Used to drive app-mode
	// auto-quit so the server exits when the last window closes.
	onCount func(n int)
}

func newHub() *hub { return &hub{clients: map[chan string]struct{}{}} }

func (h *hub) add() chan string {
	ch := make(chan string, 4)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	n := len(h.clients)
	cb := h.onCount
	h.mu.Unlock()
	if cb != nil {
		cb(n)
	}
	return ch
}

func (h *hub) remove(ch chan string) {
	h.mu.Lock()
	changed := false
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
		changed = true
	}
	n := len(h.clients)
	cb := h.onCount
	h.mu.Unlock()
	if changed && cb != nil {
		cb(n)
	}
}

func (h *hub) broadcast(msg string) {
	h.mu.Lock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default: // drop if the client is slow; it'll catch up on the next event
		}
	}
	h.mu.Unlock()
}

// handleEvents streams Server-Sent Events. The browser opens one of these and
// re-fetches whenever it receives a "refresh", so the UI stays live without a
// manual Refresh button.
func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	if a.hub == nil {
		http.Error(w, "events unavailable", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := a.hub.add()
	defer a.hub.remove(ch)

	fmt.Fprint(w, "retry: 3000\ndata: hello\n\n")
	flusher.Flush()

	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-keepAlive.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// watchLoop polls the active collection's .git metadata for changes and
// broadcasts a refresh when the signature changes. It watches cheap, decisive
// signals (HEAD, index, refs, FETCH_HEAD, merge/rebase state) so it catches
// commits, fetches, staging, branch switches and merges without scanning whole
// worktrees.
func (a *App) watchLoop() {
	if a.hub == nil {
		return
	}
	var last string
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for range tick.C {
		a.hub.mu.Lock()
		n := len(a.hub.clients)
		a.hub.mu.Unlock()
		if n == 0 {
			continue // nobody listening; don't stat the disk
		}
		sig := collectionSignature(a.root())
		if sig != last {
			if last != "" {
				a.hub.broadcast("refresh")
			}
			last = sig
		}
	}
}

func collectionSignature(root string) string {
	var b []byte
	for _, name := range findRepos(root) {
		g := filepath.Join(root, name, ".git")
		b = append(b, name...)
		for _, f := range []string{"HEAD", "index", "FETCH_HEAD", "ORIG_HEAD", "MERGE_HEAD", "packed-refs"} {
			if st, err := os.Stat(filepath.Join(g, f)); err == nil {
				b = appendInt(b, st.ModTime().UnixNano())
				b = appendInt(b, st.Size())
			} else {
				b = append(b, '0')
			}
		}
		// loose refs change on branch create/commit/fetch
		_ = filepath.WalkDir(filepath.Join(g, "refs"), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, e := d.Info(); e == nil {
				b = appendInt(b, info.ModTime().UnixNano())
			}
			return nil
		})
		b = append(b, '|')
	}
	return string(b)
}

func appendInt(b []byte, n int64) []byte {
	return append(b, byte(n), byte(n>>8), byte(n>>16), byte(n>>24), byte(n>>32))
}
