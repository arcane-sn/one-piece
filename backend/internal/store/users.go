package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionTTL = 30 * 24 * time.Hour

// CreateUser creates the one and only user.
func (s *Store) CreateUser(username, password string) error {
	username = strings.TrimSpace(username)
	switch {
	case username == "":
		return ValidationError{"username required"}
	case len(password) < 8:
		return ValidationError{"password must be at least 8 characters"}
	case len(password) > 72: // bcrypt limit
		return ValidationError{"password must be at most 72 bytes"}
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ValidationError{"a user already exists"}
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO users(username,password_hash) VALUES(?,?)`, username, string(h))
	return err
}

func (s *Store) CheckPassword(username, password string) (userID int64, ok bool) {
	var hash string
	err := s.DB.QueryRow(`SELECT id,password_hash FROM users WHERE username=?`, strings.TrimSpace(username)).Scan(&userID, &hash)
	if err != nil {
		return 0, false
	}
	return userID, bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *Store) NewSession(userID int64) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	_, err := s.DB.Exec(`INSERT INTO sessions(token,user_id,expires_at) VALUES(?,?,?)`,
		tok, userID, time.Now().Add(sessionTTL).Unix())
	return tok, err
}

func (s *Store) SessionUser(token string) (username string, ok bool) {
	err := s.DB.QueryRow(`SELECT u.username FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token=? AND s.expires_at>?`, token, time.Now().Unix()).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return "", false
	}
	return username, true
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE token=?`, token)
	return err
}
