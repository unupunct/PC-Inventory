package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

type Admin struct {
	ID          int64
	Username    string
	PasswordHash string
	CreatedAt   time.Time
	LastLogin   *time.Time
}

func (s *Store) CountAdmins(ctx context.Context) (int64, error) {
	var c int64
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins`).Scan(&c)
	return c, err
}

func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO admins (username,password_hash,created_at) VALUES (?,?,?)`, username, passwordHash, nowStr())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetAdminByUsername(ctx context.Context, username string) (*Admin, error) {
	var a Admin
	var created string
	var last sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT id,username,password_hash,created_at,last_login_at FROM admins WHERE username=?`, username).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &created, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.CreatedAt = parseTS(created)
	if last.Valid {
		t := parseTS(last.String)
		a.LastLogin = &t
	}
	return &a, nil
}

func (s *Store) UpdateAdminPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE admins SET password_hash=? WHERE id=?`, passwordHash, id)
	return err
}

func (s *Store) SetAdminLastLogin(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE admins SET last_login_at=? WHERE id=?`, nowStr(), id)
	return err
}

// Session helpers. Only the SHA-256 of the session token is stored.

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) CreateSession(ctx context.Context, token string, adminID int64, ttl time.Duration, ua string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO admin_sessions (token_hash,admin_id,created_at,expires_at,user_agent) VALUES (?,?,?,?,?)`,
		HashToken(token), adminID, nowStr(), tsStr(time.Now().UTC().Add(ttl)), ua)
	return err
}

type SessionInfo struct {
	AdminID   int64
	Username  string
	ExpiresAt time.Time
}

func (s *Store) GetSession(ctx context.Context, token string) (*SessionInfo, error) {
	var si SessionInfo
	var exp string
	err := s.DB.QueryRowContext(ctx, `SELECT s.admin_id,a.username,s.expires_at FROM admin_sessions s JOIN admins a ON a.id=s.admin_id WHERE s.token_hash=?`, HashToken(token)).
		Scan(&si.AdminID, &si.Username, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	si.ExpiresAt = parseTS(exp)
	if time.Now().UTC().After(si.ExpiresAt) {
		_ = s.DeleteSession(ctx, token)
		return nil, ErrNotFound
	}
	return &si, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM admin_sessions WHERE token_hash=?`, HashToken(token))
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at < ?`, nowStr())
	return err
}
