package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// apiKeyAuth returns middleware that requires a valid API key on every request.
// Keys are accepted as "Authorization: Bearer <key>" or "X-API-Key: <key>" and
// compared in constant time. Requests without a valid key get 401.
func apiKeyAuth(keys []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !validKey(keys, presentedKey(r)) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="redactor"`)
				writeError(w, http.StatusUnauthorized, "missing or invalid API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// presentedKey extracts the key from the Authorization or X-API-Key header.
func presentedKey(r *http.Request) string {
	if a := r.Header.Get("Authorization"); a != "" {
		if v, ok := strings.CutPrefix(a, "Bearer "); ok {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

// validKey reports whether presented matches any configured key. The comparison
// runs against every key (no early return) to keep timing independent of which
// key matched; the per-key compare itself is constant time.
func validKey(keys []string, presented string) bool {
	if presented == "" {
		return false
	}
	match := false
	for _, k := range keys {
		if subtle.ConstantTimeCompare([]byte(k), []byte(presented)) == 1 {
			match = true
		}
	}
	return match
}
