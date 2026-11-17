package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/auth"
	"github.com/pcim/pc-inventory-manager/internal/server/store"
)

type sessionKey struct{}

type sessionData struct {
	AdminID  int64
	Username string
	Token    string
}

func sessionFrom(ctx context.Context) *sessionData {
	s, _ := ctx.Value(sessionKey{}).(*sessionData)
	return s
}

// sessionAuth validates the admin session cookie or bearer token.
func (s *Server) sessionAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if c, err := r.Cookie("pcim_session"); err == nil {
			token = c.Value
		}
		if token == "" {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			}
		}
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		sess, err := s.Store.GetSession(r.Context(), token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "session expired")
			return
		}
		sd := &sessionData{AdminID: sess.AdminID, Username: sess.Username, Token: token}
		next(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, sd)))
	}
}

// recordLoginFailure counts a failed attempt for a username+IP pair. The map is
// created on first use so that a server built without going through New() still
// works, and it is pruned so it cannot grow without bound.
func (s *Server) recordLoginFailure(key string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if s.loginFails == nil {
		s.loginFails = make(map[string]loginFail)
	}
	if len(s.loginFails) > 1000 {
		now := time.Now()
		for k, v := range s.loginFails {
			if now.After(v.until) {
				delete(s.loginFails, k)
			}
		}
	}
	lf := s.loginFails[key]
	lf.count++
	lf.until = time.Now().Add(5 * time.Minute)
	s.loginFails[key] = lf
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid request")
		return
	}
	ip := clientIP(r, s.Cfg.TrustProxy)
	key := body.Username + "|" + ip
	s.loginMu.Lock()
	lf, seen := s.loginFails[key]
	s.loginMu.Unlock()
	if seen && lf.count >= 5 && time.Now().Before(lf.until) {
		writeErr(w, 429, "too many failed attempts; try again later")
		return
	}

	admin, err := s.Store.GetAdminByUsername(r.Context(), body.Username)
	if err != nil || !auth.VerifyPassword(admin.PasswordHash, body.Password) {
		s.recordLoginFailure(key)
		_ = s.Store.AddAudit(r.Context(), body.Username, "auth.login.failed", "", ip, "")
		writeErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	s.loginMu.Lock()
	delete(s.loginFails, key)
	s.loginMu.Unlock()

	token, err := auth.NewToken()
	if err != nil {
		writeErr(w, 500, "session error")
		return
	}
	ttl := time.Duration(s.Cfg.SessionTTLHours) * time.Hour
	if err := s.Store.CreateSession(r.Context(), token, admin.ID, ttl, r.UserAgent()); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.SetAdminLastLogin(r.Context(), admin.ID)
	_ = s.Store.AddAudit(r.Context(), admin.Username, "auth.login", "", ip, "")
	http.SetCookie(w, &http.Cookie{
		Name: "pcim_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.Cfg.TLSEnabled(), MaxAge: int(ttl.Seconds()),
	})
	writeJSON(w, 200, map[string]any{"ok": true, "username": admin.Username, "token": token, "expiresAt": time.Now().UTC().Add(ttl)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	sd := sessionFrom(r.Context())
	if sd != nil {
		_ = s.Store.DeleteSession(r.Context(), sd.Token)
	}
	http.SetCookie(w, &http.Cookie{Name: "pcim_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sd := sessionFrom(r.Context())
	writeJSON(w, 200, map[string]any{"username": sd.Username, "adminId": sd.AdminID, "serverName": s.serverName(r.Context())})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	sd := sessionFrom(r.Context())
	var body struct {
		Current string `json:"currentPassword"`
		New     string `json:"newPassword"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid request")
		return
	}
	admin, err := s.Store.GetAdminByUsername(r.Context(), sd.Username)
	if err != nil {
		writeErr(w, 500, "admin lookup failed")
		return
	}
	if !auth.VerifyPassword(admin.PasswordHash, body.Current) {
		writeErr(w, http.StatusForbidden, "current password is incorrect")
		return
	}
	hash, err := auth.HashPassword(body.New)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := s.Store.UpdateAdminPassword(r.Context(), admin.ID, hash); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.AddAudit(r.Context(), sd.Username, "auth.password_change", "", clientIP(r, s.Cfg.TrustProxy), "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// helpers shared by admin handlers
func (s *Server) audit(r *http.Request, action, target, details string) {
	sd := sessionFrom(r.Context())
	actor := "admin"
	if sd != nil {
		actor = sd.Username
	}
	_ = s.Store.AddAudit(r.Context(), actor, action, target, clientIP(r, s.Cfg.TrustProxy), details)
}

func (s *Server) agentOr404(w http.ResponseWriter, r *http.Request) *store.Agent {
	id := r.PathValue("id")
	a, err := s.Store.GetAgent(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "agent not found")
		return nil
	}
	return a
}
