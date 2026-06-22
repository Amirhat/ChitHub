package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

//go:embed web
var webFS embed.FS

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	var (
		port     string
		rootFlag string
		noOpen   bool
		devDir   string
		showVer  bool
	)
	flag.StringVar(&port, "port", "", "port to listen on (overrides saved config)")
	flag.StringVar(&rootFlag, "root", "", "root folder containing repositories (overrides saved config)")
	flag.BoolVar(&noOpen, "no-open", false, "do not open a browser window on start")
	flag.StringVar(&devDir, "dev", "", "serve the web UI from this directory instead of the embedded copy (development)")
	flag.BoolVar(&showVer, "version", false, "print version and exit")
	flag.Parse()

	if showVer {
		log.Printf("ChitHub %s", version)
		return
	}

	cfg := loadConfig()
	if rootFlag != "" {
		cfg.AddCollection(rootFlag)
	}
	if len(cfg.Collections) == 0 {
		// Default to the launch directory — but not "/" (e.g. when started as a
		// macOS .app, where the UI shows its "Add a folder" empty state instead).
		if wd, err := os.Getwd(); err == nil && wd != "/" {
			cfg.AddCollection(wd)
		}
	}
	if cfg.Active == "" && len(cfg.Collections) > 0 {
		cfg.Active = cfg.Collections[0]
	}
	if port != "" {
		cfg.Port = port
	}
	if cfg.Port == "" {
		cfg.Port = "7171"
	}
	saveConfig(cfg)

	app := &App{cfg: cfg, hub: newHub()}
	go app.watchLoop()

	mux := http.NewServeMux()
	app.routes(mux)

	if devDir != "" {
		log.Printf("Serving web UI from disk: %s", devDir)
		mux.Handle("/", http.FileServer(http.Dir(devDir)))
	} else {
		sub, err := fs.Sub(webFS, "web")
		if err != nil {
			log.Fatalf("embed: %v", err)
		}
		mux.Handle("/", http.FileServer(http.FS(sub)))
	}

	addr := "127.0.0.1:" + cfg.Port
	url := "http://" + addr

	log.Printf("ChitHub listening on %s", url)
	log.Printf("Active collection: %s", cfg.Active)

	// Acquire the port up front. If it's already taken, check whether it's a
	// running ChitHub: if so, just open a window pointing at it and exit; if
	// it's some unrelated process, fail with a clear message instead of opening
	// a browser to whatever is there.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if isChitHub(url) {
			log.Printf("ChitHub is already running at %s; opening a window there.", url)
			if !noOpen {
				openBrowser(url)
			}
			return
		}
		log.Fatalf("Port %s is in use by another process. Start ChitHub on a different port with -port.", cfg.Port)
	}

	srv := &http.Server{
		Handler:     logRequests(app.trackActivity(mux)),
		ReadTimeout: 15 * time.Second,
	}

	// In app mode (real window, not -dev / -no-open) tie the process lifetime to
	// the UI window so closing it quits the app and frees the port.
	if !noOpen && devDir == "" {
		app.armAutoQuit(srv)
	}
	installSignalHandlers(srv)

	if !noOpen {
		go openBrowser(url)
	}

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && len(r.URL.Path) > 4 && r.URL.Path[:4] == "/api" {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

// openBrowser tries to open the app in a dedicated "app window" (Chrome/Edge
// --app mode) so it feels like a standalone desktop app, and falls back to a
// normal browser tab.
func openBrowser(url string) {
	time.Sleep(400 * time.Millisecond)

	if runtime.GOOS == "darwin" {
		for _, browser := range []string{"Google Chrome", "Microsoft Edge", "Brave Browser", "Chromium"} {
			// Flags keep the dedicated app window from showing Chrome's first-run
			// page, "set as default" nag, or a keychain-access prompt.
			c := exec.Command("open", "-na", browser, "--args",
				"--app="+url,
				"--user-data-dir="+appWindowProfile(),
				"--no-first-run",
				"--no-default-browser-check",
				"--password-store=basic",
				"--use-mock-keychain",
				"--window-size=1280,860",
			)
			if err := c.Run(); err == nil {
				return
			}
		}
		if err := exec.Command("open", url).Start(); err != nil {
			log.Printf("Could not open a browser automatically; open %s manually.", url)
		}
		return
	}

	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}

func appWindowProfile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	p := filepath.Join(home, ".chithub-window")
	_ = os.MkdirAll(p, 0o755)
	return p
}
