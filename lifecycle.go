package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// In "app" mode (launched as the .app / `chithub` with no special flags) the
// server's lifetime is tied to the UI window. The web UI holds an SSE
// connection (/api/events) for as long as its window is open, and posts an
// explicit /api/window-closed beacon when it is closing. Either signal makes
// the process exit, so ChitHub behaves like a real desktop app:
//
//   - closing the window quits the app (after a short grace, so a reload that
//     reconnects within it doesn't kill the server),
//   - the listening port is freed, so the app relaunches cleanly,
//   - you never have to Force Quit it.
//
// Two graces balance the two ways a window can "go away":
//   - closeGrace (short): the page told us it's closing — exit promptly.
//   - dropGrace  (long):  the SSE connection merely dropped (a network blip, a
//     sleep/wake, a throttled background tab) — wait longer so a reconnect can
//     cancel the quit and we don't kill a window the user still has open.
//
// Dev/server modes (-dev, -no-open) never arm any of this, so they stay up.
const (
	defaultFirstGrace = 90 * time.Second // backstop: no window ever connected
	defaultDropGrace  = 20 * time.Second // SSE dropped without an explicit close
	defaultCloseGrace = 2 * time.Second  // explicit window-closed beacon
)

// armAutoQuit wires the app to exit when no UI window is connected.
func (a *App) armAutoQuit(srv *http.Server) {
	a.srv = srv
	a.quitMu.Lock()
	if a.firstGrace == 0 {
		a.firstGrace = defaultFirstGrace
	}
	if a.dropGrace == 0 {
		a.dropGrace = defaultDropGrace
	}
	if a.closeGrace == 0 {
		a.closeGrace = defaultCloseGrace
	}
	// If a window never connects (e.g. the browser failed to open), don't hang
	// around forever holding the port. The first HTTP request cancels this (see
	// trackActivity) — long before the SSE stream connects.
	a.armQuitLocked(a.firstGrace, "no window ever connected")
	a.quitMu.Unlock()

	a.appMode.Store(true)
	if a.hub != nil {
		a.hub.mu.Lock()
		a.hub.onCount = a.uiClientsChanged
		a.hub.mu.Unlock()
	}
}

// trackActivity cancels the startup backstop as soon as the window makes its
// first request (it has clearly opened). After that, the SSE connection and the
// window-closed beacon drive shutdown.
func (a *App) trackActivity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.appMode.Load() && !a.sawActivity.Swap(true) {
			a.quitMu.Lock()
			a.stopTimerLocked()
			a.quitMu.Unlock()
		}
		next.ServeHTTP(w, r)
	})
}

// uiClientsChanged is invoked by the hub with the live UI-window count.
func (a *App) uiClientsChanged(n int) {
	if !a.appMode.Load() {
		return
	}
	a.quitMu.Lock()
	defer a.quitMu.Unlock()
	a.stopTimerLocked()
	if n > 0 {
		a.closing.Store(false) // a window is connected again (e.g. a reload)
		return
	}
	grace := a.dropGrace
	if a.closing.Load() {
		grace = a.closeGrace
	}
	a.armQuitLocked(grace, "window closed")
}

// handleWindowClosed receives the beacon the UI sends as its window is closing,
// so we can quit promptly instead of waiting out the long SSE-drop grace.
func (a *App) handleWindowClosed(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
	if !a.appMode.Load() {
		return
	}
	a.closing.Store(true)
	a.quitMu.Lock()
	a.armQuitLocked(a.closeGrace, "window closed")
	a.quitMu.Unlock()
}

// armQuitLocked (re)arms the quit timer. Caller must hold quitMu.
func (a *App) armQuitLocked(grace time.Duration, reason string) {
	a.stopTimerLocked()
	a.quitTimer = time.AfterFunc(grace, func() { a.quitNow(reason) })
}

func (a *App) stopTimerLocked() {
	if a.quitTimer != nil {
		a.quitTimer.Stop()
		a.quitTimer = nil
	}
}

// quitNow shuts the server down and exits — unless a window is (still or again)
// connected, in which case a just-fired timer must not win the race.
func (a *App) quitNow(reason string) {
	a.quitMu.Lock()
	if a.hub != nil {
		a.hub.mu.Lock()
		n := len(a.hub.clients)
		a.hub.mu.Unlock()
		if n > 0 {
			a.quitMu.Unlock()
			return
		}
	}
	a.quitMu.Unlock()

	if a.onQuit != nil { // test hook
		a.onQuit(reason)
		return
	}
	log.Printf("Shutting down: %s.", reason)
	if a.srv != nil {
		_ = a.srv.Close()
	}
	os.Exit(0)
}

// installSignalHandlers makes SIGINT/SIGTERM (Ctrl-C, `kill`, Activity Monitor's
// Quit) shut the server down gracefully instead of leaving it half-running.
func installSignalHandlers(srv *http.Server) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		log.Print("Signal received; shutting down.")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		os.Exit(0)
	}()
}

// isChitHub probes a base URL to confirm a running ChitHub instance is behind it
// (so a busy port isn't mistaken for one when it's some unrelated process).
func isChitHub(base string) bool {
	c := &http.Client{Timeout: time.Second}
	resp, err := c.Get(base + "/api/config")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
