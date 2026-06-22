//go:build !darwin

package main

// On non-macOS platforms there is no native WebView shell; ChitHub opens the UI
// in the browser instead (see openBrowser). These stubs keep main.go portable.

func nativeWindowSupported() bool { return false }

func appIcon() []byte { return nil }

func runNativeWindow(url, title string, icon []byte, w, h int) {}
