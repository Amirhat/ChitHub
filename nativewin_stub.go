//go:build !darwin || !cgo

package main

// Fallback for any build without the native macOS WebView: non-macOS platforms,
// and macOS builds compiled with CGO disabled. ChitHub then opens the UI in the
// browser (see openBrowser / nativeMode in main.go). These stubs keep the build
// compiling in every configuration.

func nativeWindowSupported() bool { return false }

func appIcon() []byte { return nil }

func runNativeWindow(url, title string, icon []byte, w, h int) {}
