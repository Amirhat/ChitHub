# Contributing to ChitHub

Thanks for your interest in making ChitHub better! 🐙 Bug reports, feature
ideas, documentation fixes and code are all welcome.

## Ways to contribute

- 🐛 **Report a bug** — open a [Bug report](https://github.com/Amirhat/ChitHub/issues/new?template=bug_report.yml).
- 💡 **Request a feature** — open a [Feature request](https://github.com/Amirhat/ChitHub/issues/new?template=feature_request.yml).
- 💬 **Ask a question or float an idea** — start a [Discussion](https://github.com/Amirhat/ChitHub/discussions).
- 🔧 **Send a pull request** — see below.

For anything bigger than a small fix, please **open an issue first** so we can
agree on the approach before you invest time in it.

## Development setup

You need **Go 1.24+** and **git** on your `PATH`. Building the native macOS
window also needs the Xcode command-line tools (`xcode-select --install`).

```bash
git clone https://github.com/Amirhat/ChitHub
cd ChitHub
go run . -dev ./web      # run with the web UI served live from disk
```

`-dev` serves `web/` from disk, so you can edit `index.html` / `style.css` /
`app.js` and just reload the window — no rebuild needed. Go changes need a
restart. Other handy flags: `-root <dir>`, `-port <n>`, `-browser` (use a
browser instead of the native window), `-no-open`.

## Project layout

| Path | What it is |
|------|------------|
| `main.go` | Startup, native-window vs browser launch, server lifecycle |
| `git.go`, `gitext.go`, `features.go` | Git operations (everything shells out to real `git`) |
| `handlers*.go` | The JSON HTTP API |
| `watch.go` | The Server-Sent-Events live-refresh hub |
| `nativewin_darwin.go` | The macOS WKWebView shell (cgo) |
| `web/` | The dependency-free UI (`index.html`, `style.css`, `app.js`) |
| `*_test.go` | The end-to-end test suite |

ChitHub has **no runtime dependencies** by design — please keep it that way
(no JS frameworks, and no new Go modules unless there's truly no alternative).

## Before opening a pull request

```bash
go test ./...           # end-to-end suite — runs offline against temp git repos
go vet ./...
gofmt -l .              # should print nothing
node --check web/app.js
```

- Match the style, naming and comment density of the surrounding code.
- Keep each commit focused, and write a message that explains the **why**.
- If you change behaviour, add or update a test for it.
- Reference the issue you're addressing (e.g. "Closes #123") in the PR.

## Reporting security issues

Please **don't** open a public issue for security problems — see
[SECURITY.md](SECURITY.md) for how to report them privately.

## Code of Conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). By taking
part, you agree to uphold it.
