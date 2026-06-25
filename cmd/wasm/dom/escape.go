// Package dom provides helpers for interacting with the browser DOM from Go WASM.
// Escape and other pure helpers in this package are buildable under any GOOS;
// the js+wasm-tagged files (event.go, fetch.go) require GOOS=js GOARCH=wasm.
package dom

import "strings"

// Escape replaces HTML-special characters with their entity equivalents.
// The replacement order (&, <, >, ", ') is significant: & must come first to
// avoid double-escaping the ampersands that the subsequent replacements
// introduce. The double-quote and single-quote escapes cover both attribute
// quoting styles.
func Escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
