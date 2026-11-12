// Package api is the JSON HTTP layer.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

type server struct {
	st      *store.Store
	static  string
	mu      sync.Mutex
	turns   map[int64]context.CancelFunc // running brainstorm turns by board id
	runs    map[int64]context.CancelFunc // running Agent Task executions by item id
	chains  map[int64]context.CancelFunc // Run All chains by card id
	discuss map[int64]context.CancelFunc // running Discuss turns by card id
}

// New builds the handler: /api/* (authenticated) plus static files from staticDir.
func New(st *store.Store, staticDir string) http.Handler {
	s := &server{st: st, static: staticDir, turns: map[int64]context.CancelFunc{}, runs: map[int64]context.CancelFunc{}, chains: map[int64]context.CancelFunc{}, discuss: map[int64]context.CancelFunc{}}
	if err := st.FailRunningReviews(); err != nil { // reviews cut short by a previous shutdown
		log.Printf("sweeping stale reviews: %v", err)
	}
	if err := st.FailRunningBrainstorm(); err != nil { // chat replies cut short by a previous shutdown
		log.Printf("sweeping stale brainstorm replies: %v", err)
	}
	if err := st.FailRunningDiscuss(); err != nil { // Discuss replies cut short by a previous shutdown
		log.Printf("sweeping stale discuss replies: %v", err)
	}
	if err := st.FailRunningJobs(); err != nil { // Generate jobs cut short by a previous shutdown
		log.Printf("sweeping stale jobs: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("POST /api/pick-folder", s.pickFolder)
	mux.HandleFunc("POST /api/generate-description", s.generateDescription)
	mux.HandleFunc("GET /api/boards", s.listBoards)
	mux.HandleFunc("POST /api/boards", s.createBoard)
	mux.HandleFunc("GET /api/boards/{id}", s.getBoard)
	mux.HandleFunc("PUT /api/boards/{id}", s.updateBoard)
	mux.HandleFunc("DELETE /api/boards/{id}", s.deleteBoard)
	mux.HandleFunc("GET /api/boards/{id}/files", s.listFiles)
	mux.HandleFunc("GET /api/boards/{id}/brainstorm", s.getBrainstorm)
	mux.HandleFunc("POST /api/boards/{id}/brainstorm/messages", s.postBrainstormMessage)
	mux.HandleFunc("POST /api/boards/{id}/brainstorm/stop", s.stopBrainstorm)
	mux.HandleFunc("POST /api/boards/{id}/brainstorm/new", s.newBrainstorm)
	mux.HandleFunc("GET /api/boards/{id}/plan/{v}", s.getPlanVersion)
	mux.HandleFunc("POST /api/boards/{id}/plan/{v}/restore", s.restorePlan)
	mux.HandleFunc("GET /api/boards/{id}/cards", s.listCards)
	mux.HandleFunc("POST /api/boards/{id}/cards", s.createCard)
	mux.HandleFunc("PUT /api/cards/{id}", s.updateCard)
	mux.HandleFunc("POST /api/cards/{id}/move", s.moveCard)
	mux.HandleFunc("GET /api/cards/{id}/review", s.getReview)
	mux.HandleFunc("POST /api/cards/{id}/review", s.postReview)
	mux.HandleFunc("POST /api/cards/{id}/approve", s.approveCard)
	mux.HandleFunc("DELETE /api/cards/{id}", s.deleteCard)
	mux.HandleFunc("POST /api/uploads", s.uploadImage)
	mux.HandleFunc("GET /api/uploads/{name}", s.getUpload)
	mux.HandleFunc("POST /api/flows/generate", s.generateFlow)
	mux.HandleFunc("GET /api/cards/{id}/flows", s.listFlows)
	mux.HandleFunc("POST /api/cards/{id}/flows", s.createFlow)
	mux.HandleFunc("PUT /api/flows/{id}", s.updateFlow)
	mux.HandleFunc("DELETE /api/flows/{id}", s.deleteFlow)
	mux.HandleFunc("GET /api/cards/{id}/branches", s.listBranches)
	mux.HandleFunc("POST /api/cards/{id}/branches", s.planBranch)
	mux.HandleFunc("PUT /api/cards/{id}/branches", s.updateBranch)
	mux.HandleFunc("DELETE /api/cards/{id}/branches", s.deleteBranch)
	mux.HandleFunc("GET /api/cards/{id}/discuss", s.getDiscuss)
	mux.HandleFunc("POST /api/cards/{id}/discuss/messages", s.postDiscussMessage)
	mux.HandleFunc("POST /api/cards/{id}/discuss/stop", s.stopDiscussHandler)
	mux.HandleFunc("POST /api/cards/{id}/discuss/new", s.newDiscuss)
	mux.HandleFunc("GET /api/cards/{id}/items", s.listItems)
	mux.HandleFunc("POST /api/cards/{id}/items", s.createItem)
	mux.HandleFunc("POST /api/cards/{id}/generate-items", s.generateItems)
	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("GET /api/jobs/active", s.activeJobs)
	mux.HandleFunc("POST /api/jobs/{id}/seen", s.markJob("seen"))
	mux.HandleFunc("POST /api/jobs/{id}/consume", s.markJob("consumed"))
	mux.HandleFunc("POST /api/items/{id}/restore", s.restoreItem)
	mux.HandleFunc("PUT /api/items/{id}", s.updateItem)
	mux.HandleFunc("POST /api/cards/{id}/run-all", s.runAll)
	mux.HandleFunc("POST /api/cards/{id}/stop-all", s.stopAll)
	mux.HandleFunc("POST /api/items/{id}/run", s.runItem)
	mux.HandleFunc("POST /api/items/{id}/stop", s.stopItem)
	mux.HandleFunc("DELETE /api/items/{id}", s.deleteItem)
	mux.HandleFunc("GET /api/items/{id}/flows", s.listItemFlows)
	mux.HandleFunc("POST /api/items/{id}/flows", s.createItemFlow)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	if staticDir != "" {
		mux.Handle("/", s.spa())
	}
	return s.guard(mux)
}

// spa serves files from the static dir, falling back to index.html so
// client-side routes (e.g. /boards/1) survive a refresh.
func (s *server) spa() http.Handler {
	files := http.FileServer(http.Dir(s.static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// path.Clean on a rooted path neutralizes any ".." traversal.
		rel := path.Clean("/" + r.URL.Path)
		if fi, err := os.Stat(filepath.Join(s.static, filepath.FromSlash(rel))); err == nil && !fi.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(s.static, "index.html"))
	})
}

// guard enforces, for /api/*: JSON content type on mutations (cheap CSRF defence),
// a 1 MiB body cap (16 MiB for image uploads), and a valid session (except login).
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
					writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "content-type must be application/json"})
					return
				}
			}
			limit := int64(1 << 20)
			if r.URL.Path == "/api/uploads" {
				limit = 16 << 20 // a base64 image up to 10 MB
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
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
	var ce store.ConflictError
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case errors.As(err, &ce):
		writeJSON(w, http.StatusConflict, map[string]string{"error": ce.Msg})
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
