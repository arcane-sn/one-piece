// Package api is the JSON HTTP layer.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net/http"
	"strings"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

type server struct {
	st     *store.Store
	static string
}

// New builds the handler: /api/* (authenticated) plus static files from staticDir.
func New(st *store.Store, staticDir string) http.Handler {
	s := &server{st: st, static: staticDir}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("GET /api/boards", s.listBoards)
	mux.HandleFunc("POST /api/boards", s.createBoard)
	mux.HandleFunc("GET /api/boards/{id}", s.getBoard)
	mux.HandleFunc("PUT /api/boards/{id}", s.updateBoard)
	mux.HandleFunc("DELETE /api/boards/{id}", s.deleteBoard)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	return s.guard(mux)
}

// guard enforces, for /api/*: JSON content type on mutations (cheap CSRF defence),
// a 1 MiB body cap, and a valid session (except login).
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
					writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "content-type must be application/json"})
					return
				}
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			if r.URL.Path != "/api/login" {
				if _, ok := s.currentUser(r); !ok {
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) currentUser(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", false
	}
	return s.st.SessionUser(c.Value)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

func writeErr(w http.ResponseWriter, err error) {
	var ve store.ValidationError
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ve.Msg})
	default:
		log.Printf("internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
	}
}

// decode reads a JSON body into v; bad JSON is a ValidationError (400).
func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return store.ValidationError{Msg: "invalid JSON body"}
	}
	return nil
}
