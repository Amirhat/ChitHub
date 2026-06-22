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
// connection (/api/events) for as long as its window is open; when the last
// window closes the connection drops, and shortly after that the process
// exits. This is what makes ChitHub behave like a real desktop app:
//
//   - closing the window quits the app (no lingering background process),
//   - the listening port is freed, so the app relaunches cleanly,
//   - you never have to Force Quit it.
//
// Dev/server modes (-dev, -no-open) do NOT arm this, so they stay up.
const (
	defaultFirstGrace = 90 * time.Second // window must connect within this of launch
	defaultQuitGrace  = 5 * time.Second  // quit this long after the last window closes
)

// armAutoQuit wires the app to exit when no UI window is connected.
func (a *App) armAutoQuit(srv *http.Server) {
	a.srv = srv
	a.autoQuit = true
	if a.quitGrace == 0 {
		a.quitGrace = defaultQuitGrace
	}
	if a.firstGrace == 0 {
		a.firstGrace = defaultFirstGrace
	}
	if a.hub != nil {
		a.hub.onCount = a.uiClientsChanged
	}
	// If the window never connects (e.g. the browser failed to open), don't hang
	// around forever holding the port.
	a.quitMu.Lock()
	a.quitTimer = time.AfterFunc(a.firstGrace, func() { a.quitNow("window never connected") })
	a.quitMu.Unlock()
}

// uiClientsChanged is invoked by the hub with the live UI-window count.
func (a *App) uiClientsChanged(n int) {
	if !a.autoQuit {
		return
	}
	a.quitMu.Lock()
	defer a.quitMu.Unlock()
	if a.quitTimer != nil {
		a.quitTimer.Stop()
		a.quitTimer = nil
	}
	if n > 0 {
		return // a window is connected — stay alive
	}
	// No window connected: arm the grace timer. A page reload reconnects well
	// within the grace period, so this only fires on a genuine close.
	a.quitTimer = time.AfterFunc(a.quitGrace, func() { a.quitNow("window closed") })
}

// quitNow shuts the server down and exits — unless a window reconnected in the
// meantime (e.g. a slow reload).
func (a *App) quitNow(reason string) {
	if a.hub != nil {
		a.hub.mu.Lock()
		n := len(a.hub.clients)
		a.hub.mu.Unlock()
		if n > 0 {
			return
		}
	}
	if a.onQuit != nil { // test hook
		a.onQuit(reason)
		return
	}
	log.Printf("Shutting down: %s.", reason)
	if a.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = a.srv.Shutdown(ctx)
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
