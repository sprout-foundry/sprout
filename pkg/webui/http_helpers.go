//go:build !js

package webui

import (
	"net/http"
)

// requireMethod checks that the request method matches the allowed method.
// On mismatch it writes a JSON 405 error and returns false; the caller
// should return immediately. On match it returns true and the handler
// proceeds normally.
//
// Usage:
//
//	func (ws *ReactWebServer) handleAPIFoo(w http.ResponseWriter, r *http.Request) {
//	    if !requireMethod(w, r, http.MethodGet) {
//	        return
//	    }
//	    // ... handler body
//	}
func requireMethod(w http.ResponseWriter, r *http.Request, allowed string) bool {
	if r.Method == allowed {
		return true
	}
	writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	return false
}

// requireMethods is the multi-method variant for handlers that accept
// both GET and HEAD, or POST and PUT, etc.
func requireMethods(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	return false
}
