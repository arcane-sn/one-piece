package store

import (
	"errors"
	"testing"
)

func isValidation(err error) bool {
	var ve ValidationError
	return errors.As(err, &ve)
}

func TestCreateUserRejectsShortPassword(t *testing.T) {
	s := openTemp(t)
	if err := s.CreateUser("owner", "short"); !isValidation(err) {
		t.Fatalf("want ValidationError, got %v", err)
	}
}

func TestSecondUserRejected(t *testing.T) {
	s := openTemp(t)
	if err := s.CreateUser("owner", "password123"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser("other", "password123"); !isValidation(err) {
		t.Fatalf("want ValidationError for second user, got %v", err)
	}
}

func TestCheckPassword(t *testing.T) {
	s := openTemp(t)
	s.CreateUser("owner", "password123")
	if _, ok := s.CheckPassword("owner", "password123"); !ok {
		t.Error("correct password rejected")
	}
	if _, ok := s.CheckPassword("owner", "wrong-pass"); ok {
		t.Error("wrong password accepted")
	}
	if _, ok := s.CheckPassword("nobody", "password123"); ok {
		t.Error("unknown user accepted")
	}
}

func TestSessionExpires(t *testing.T) {
	s := openTemp(t)
	s.CreateUser("owner", "password123")
	id, _ := s.CheckPassword("owner", "password123")
	tok, err := s.NewSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if name, ok := s.SessionUser(tok); !ok || name != "owner" {
		t.Fatalf("fresh session: %q %v", name, ok)
	}
	s.DB.Exec(`UPDATE sessions SET expires_at=1 WHERE token=?`, tok)
	if _, ok := s.SessionUser(tok); ok {
		t.Error("expired session still valid")
	}
}
