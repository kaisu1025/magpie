package gui

import (
	"net/http"
	"strings"
)

// apiFramed is whether r is a request for /api that no page of the window's
// own makes: one from a sandboxed frame (Origin: null, which a plugin's
// page's fetch would send were its policy not to stop it), one that loads
// /api into a frame, or one another site's page sends. The desktop
// window's /api has no key, so this is what keeps a plugin's page out.
func apiFramed(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	if r.Header.Get("Origin") == "null" {
		return true
	}
	switch r.Header.Get("Sec-Fetch-Dest") {
	case "iframe", "frame", "embed", "object":
		return true
	}
	return r.Header.Get("Sec-Fetch-Site") == "cross-site" && r.Header.Get("Sec-Fetch-Dest") != "document"
}
