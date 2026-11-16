// Package auth implements password login and cookie sessions (SPEC §4 users/sessions).
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// CookieName is the session cookie.
const CookieName = "mt_session"

// SessionTTL is the session lifetime.
const SessionTTL = 12 * time.Hour

// ErrInvalidCredentials is returned for a wrong username or password.
var ErrInvalidCredentials = errors.New("invalid credentials")

// User is the logged-in principal.
type User struct {
	Username    string `json:"username"`
	Role        string `json:"role"`
	DisplayName string `json:"display_name"`
}

// Service manages sessions.
type Service struct {
	Pool *pgxpool.Pool
}

// Login checks the password and creates a session token.
func (s *Service) Login(ctx context.Context, username, password string) (*User, string, error) {
	var u User
	var hash string
	err := s.Pool.QueryRow(ctx, `SELECT username, role, display_name, password_hash FROM users WHERE username=$1`, username).
		Scan(&u.Username, &u.Role, &u.DisplayName, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrInvalidCredentials
	}
	if err != nil {
		return nil, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, "", ErrInvalidCredentials
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, "", err
	}
	token := hex.EncodeToString(buf)
	if _, err := s.Pool.Exec(ctx, `INSERT INTO sessions(token, username, expires_at) VALUES ($1,$2,$3)`,
		token, u.Username, time.Now().Add(SessionTTL)); err != nil {
		return nil, "", err
	}
	// Opportunistic cleanup of expired sessions.
	_, _ = s.Pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
	return &u, token, nil
}

// Logout deletes a session.
func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE token=$1`, token)
	return err
}

// Lookup resolves a session token into a user; nil when absent or expired.
func (s *Service) Lookup(ctx context.Context, token string) (*User, error) {
	if token == "" {
		return nil, nil
	}
	var u User
	err := s.Pool.QueryRow(ctx, `SELECT u.username, u.role, u.display_name FROM sessions s
		JOIN users u ON u.username = s.username WHERE s.token=$1 AND s.expires_at > now()`, token).
		Scan(&u.Username, &u.Role, &u.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// SetCookie writes the session cookie.
func SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: int(SessionTTL / time.Second),
	})
}

// ClearCookie removes the session cookie.
func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

type ctxKey struct{}

// WithUser stores the user in the context.
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// FromContext returns the user stored by WithUser.
func FromContext(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}
