package api

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// metricsAuth protects GET /metrics. Prometheus cannot send a session cookie,
// so the endpoint accepts a dedicated scrape token that the admin copies from
// the Settings page, either as ?token=… or as a Bearer header. When no token is
// configured the endpoint falls back to a normal admin session, and is closed
// entirely if neither is present.
func (s *Server) metricsAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		want := s.metricsToken(r)
		if want == "" {
			// No scrape token configured: require an admin session.
			s.sessionAuth(next)(w, r)
			return
		}
		got := strings.TrimSpace(r.URL.Query().Get("token"))
		if got == "" {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				got = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			}
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="pcim-metrics"`)
			http.Error(w, "invalid metrics token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// metricsToken returns the scrape token: the stored setting first, then the
// PCIM_METRICS_TOKEN environment variable.
func (s *Server) metricsToken(r *http.Request) string {
	if v, ok := s.Store.GetSetting(r.Context(), "metrics_token"); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(os.Getenv("PCIM_METRICS_TOKEN"))
}
