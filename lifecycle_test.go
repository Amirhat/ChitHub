package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestEventsStreamDrivesClientCount proves the real /api/events SSE endpoint
// registers a connected window in the hub and deregisters it when the
// connection drops — the signal that drives app-mode auto-quit.
func TestEventsStreamDrivesClientCount(t *testing.T) {
	counts := make(chan int, 8)
	app := &App{cfg: Config{Collections: []string{t.TempDir()}}, hub: newHub()}
	app.hub.onCount = func(n int) { counts <- n }
	mux := http.NewServeMux()
	app.routes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = resp.Body.Read(make([]byte, 16)) // read the initial "hello" so the handler is live

	if n := <-counts; n != 1 {
		t.Fatalf("on connect: client count = %d, want 1", n)
	}

	cancel() // the window closes
	resp.Body.Close()
	select {
	case n := <-counts:
		if n != 0 {
			t.Fatalf("on disconnect: client count = %d, want 0", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hub never reported the window disconnecting")
	}
}

// newAutoQuitApp returns an app wired for auto-quit with an injected quit hook
// (so tests observe shutdown instead of calling os.Exit), and a tiny grace.
func newAutoQuitApp(grace time.Duration) (*App, chan string) {
	quit := make(chan string, 1)
	a := &App{hub: newHub(), autoQuit: true, quitGrace: grace, firstGrace: time.Hour}
	a.onQuit = func(reason string) { quit <- reason }
	a.hub.onCount = a.uiClientsChanged
	return a, quit
}

func TestAutoQuitAfterLastWindowCloses(t *testing.T) {
	a, quit := newAutoQuitApp(40 * time.Millisecond)
	c1 := a.hub.add()
	c2 := a.hub.add()

	a.hub.remove(c1) // one window still open -> must NOT quit
	select {
	case r := <-quit:
		t.Fatalf("quit while a window was still open: %q", r)
	case <-time.After(150 * time.Millisecond):
	}

	a.hub.remove(c2) // last window closed -> quit after the grace
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("did not quit after the last window closed")
	}
}

func TestAutoQuitCancelledByReconnect(t *testing.T) {
	a, quit := newAutoQuitApp(100 * time.Millisecond)
	c := a.hub.add()
	a.hub.remove(c)                  // arm the grace timer
	time.Sleep(20 * time.Millisecond)
	_ = a.hub.add()                  // a reload reconnects before the grace elapses

	select {
	case r := <-quit:
		t.Fatalf("quit despite a reconnect: %q", r)
	case <-time.After(220 * time.Millisecond):
	}
}

func TestNoAutoQuitWhenDisabled(t *testing.T) {
	a := &App{hub: newHub()} // autoQuit defaults to false (dev / -no-open mode)
	quit := make(chan string, 1)
	a.onQuit = func(reason string) { quit <- reason }
	a.hub.onCount = a.uiClientsChanged

	c := a.hub.add()
	a.hub.remove(c)
	select {
	case <-quit:
		t.Fatal("auto-quit fired even though it was disabled")
	case <-time.After(120 * time.Millisecond):
	}
}
