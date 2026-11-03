package api

import (
	"net/http"
	"time"
)

const (
	cookieName = "op_session"
	failDelay  = 300 * time.Millisecond // slows password guessing
)

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	id, ok := s.st.CheckPassword(in.Username, in.Password)
	if !ok {
		time.Sleep(failDelay)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	tok, err := s.st.NewSession(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.st.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	name, _ := s.currentUser(r)
	writeJSON(w, http.StatusOK, map[string]string{"username": name})
}
