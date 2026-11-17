package api

// Agent update endpoints. The agent asks what is available, then downloads the
// binary over the authenticated channel. The server only serves files from its
// own release folder and only when the admin enabled auto-update.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/pcim/pc-inventory-manager/internal/server/store"
)

func (s *Server) handleAgentUpdateCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.Store.AgentUpdatesEnabled(ctx) {
		writeJSON(w, 200, map[string]any{"enabled": false})
		return
	}
	// Authenticated by the agent middleware, so the agent comes from the
	// request context rather than the URL path.
	a := agentFrom(ctx)
	if a == nil {
		writeErr(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	platform := r.URL.Query().Get("platform")
	arch := r.URL.Query().Get("arch")
	if platform == "" {
		platform = a.Platform
	}
	if arch == "" {
		arch = a.Arch
	}
	up, err := s.Store.AgentUpdateFor(ctx, platform, arch)
	if err != nil {
		writeJSON(w, 200, map[string]any{"enabled": true, "update": nil, "message": "no release published for " + platform + "/" + arch})
		return
	}
	current := strings.TrimSpace(r.URL.Query().Get("current"))
	if current == "" {
		current = a.AgentVersion
	}
	if up.Version == current {
		writeJSON(w, 200, map[string]any{"enabled": true, "update": nil, "message": "agent is current"})
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": true, "update": up})
}

func (s *Server) handleAgentUpdateDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.Store.AgentUpdatesEnabled(ctx) {
		http.NotFound(w, r)
		return
	}
	if agentFrom(r.Context()) == nil {
		writeErr(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	platform := r.URL.Query().Get("platform")
	arch := r.URL.Query().Get("arch")
	up, err := s.Store.AgentUpdateFor(ctx, platform, arch)
	if err != nil {
		writeErr(w, 404, "no update published")
		return
	}
	path := store.UpdateBinaryPath(s.Cfg.DataDir, up)
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, 404, "update file is missing on the server")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprint(st.Size()))
	w.Header().Set("X-Agent-Version", up.Version)
	w.Header().Set("X-Agent-SHA256", up.SHA256)
	w.Header().Set("Content-Disposition", `attachment; filename="`+up.Filename+`"`)
	s.audit(r, "agent.update.download", platform+"/"+arch, up.Version)
	_, _ = io.Copy(w, f)
}
