package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/qa"
	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func (s *server) qaPaths() qa.Paths { return qa.Paths{Data: s.st.Dir} }

// qaTryLock takes the one app-wide QA lock (a run, the headed login or a session check); false when held.
func (s *server) qaTryLock(what string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.qaBusy != "" {
		return false
	}
	s.qaBusy = what
	return true
}

func (s *server) qaUnlock() {
	s.mu.Lock()
	s.qaBusy = ""
	s.mu.Unlock()
}

func (s *server) qaHeld() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.qaBusy
}

// sweepQA removes run folders whose rows are gone (a deleted item or card) and clears a stale stack.
func sweepQA(st *store.Store) {
	p := qa.Paths{Data: st.Dir}
	qa.CleanupStale(p.StackFile())
	ids, err := st.QARunIDs()
	if err != nil {
		return
	}
	ents, _ := os.ReadDir(p.Runs())
	for _, e := range ents {
		if id, err := strconv.ParseInt(e.Name(), 10, 64); err != nil || !ids[id] {
			os.RemoveAll(filepath.Join(p.Runs(), e.Name()))
		}
	}
}

type qaSession struct {
	State   string `json:"state"` // none | valid | expired
	SavedAt string `json:"savedAt"`
}

func (s *server) sessionState(boardID int64) qaSession {
	f := s.qaPaths().Auth(boardID)
	fi, err := os.Stat(f)
	if err != nil {
		return qaSession{State: "none"}
	}
	b, _ := os.ReadFile(f)
	st := "valid"
	if qa.StateExpired(b, time.Now().Unix()) {
		st = "expired"
	}
	return qaSession{State: st, SavedAt: fi.ModTime().UTC().Format(time.RFC3339)}
}

type qaView struct {
	Config    qa.Config `json:"config"`
	Toolchain bool      `json:"toolchain"`
	Session   qaSession `json:"session"`
	Busy      string    `json:"busy"`
}

func (s *server) qaBoard(w http.ResponseWriter, r *http.Request) (store.Board, bool) {
	id, err := pathID(r)
	var b store.Board
	if err == nil {
		b, err = s.st.GetBoard(id)
	}
	if err != nil {
		writeErr(w, err)
		return b, false
	}
	return b, true
}

func (s *server) view(b store.Board) (qaView, error) {
	cfg, err := s.st.GetQAConfig(b.ID)
	return qaView{Config: cfg, Toolchain: s.qaPaths().ToolchainReady(), Session: s.sessionState(b.ID), Busy: s.qaHeld()}, err
}

// GET /api/boards/{id}/qa
func (s *server) getQA(w http.ResponseWriter, r *http.Request) {
	b, ok := s.qaBoard(w, r)
	if !ok {
		return
	}
	v, err := s.view(b)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// PUT /api/boards/{id}/qa: the owner replaces the stack config (Claude has no way to reach this).
func (s *server) putQA(w http.ResponseWriter, r *http.Request) {
	b, ok := s.qaBoard(w, r)
	if !ok {
		return
	}
	var in qa.Config
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.st.SetQAConfig(b.ID, in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := s.view(b)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// qaLogin runs the headed login script until the owner closes the window. A variable so tests never open a browser.
var qaLogin = func(ctx context.Context, p qa.Paths, boardID int64, loginURL string) error {
	if err := os.MkdirAll(p.AuthDir(), 0o700); err != nil {
		return err
	}
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "TMPDIR=" + os.TempDir(), "PLAYWRIGHT_BROWSERS_PATH=" + p.Browsers()}
	_, err := qa.Exec(ctx, p.Runner(), env, "node", filepath.Join(p.Runner(), "login.mjs"), loginURL, p.Auth(boardID))
	return err
}

// POST /api/boards/{id}/qa/login: open a headed Chromium for the owner to log in by hand.
func (s *server) qaLoginHandler(w http.ResponseWriter, r *http.Request) {
	b, ok := s.qaBoard(w, r)
	if !ok {
		return
	}
	cfg, _ := s.st.GetQAConfig(b.ID)
	if !cfg.Configured() {
		writeErr(w, store.ConflictError{Msg: "Save the QA stack first."})
		return
	}
	p := s.qaPaths()
	if !p.ToolchainReady() {
		writeErr(w, store.ConflictError{Msg: "Playwright is not installed. Run `server qa-setup` once."})
		return
	}
	if !s.qaTryLock("login") {
		writeErr(w, store.ConflictError{Msg: "Another QA action is in progress."})
		return
	}
	go func() {
		defer s.qaUnlock()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		if err := qaLogin(ctx, p, b.ID, cfg.LoginURL); err != nil {
			log.Printf("qa login board %d: %v", b.ID, err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

// POST /api/boards/{id}/qa/session/check: judge the saved login from the file alone (the live check
// needs the stack and happens at the start of each run).
func (s *server) qaCheckHandler(w http.ResponseWriter, r *http.Request) {
	b, ok := s.qaBoard(w, r)
	if !ok {
		return
	}
	v, err := s.view(b)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// DELETE /api/boards/{id}/qa/session: forget the saved login.
func (s *server) qaForgetHandler(w http.ResponseWriter, r *http.Request) {
	b, ok := s.qaBoard(w, r)
	if !ok {
		return
	}
	if s.qaHeld() != "" {
		writeErr(w, store.ConflictError{Msg: "Another QA action is in progress."})
		return
	}
	os.Remove(s.qaPaths().Auth(b.ID))
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/items/{id}/qa-runs
func (s *server) listQARuns(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err == nil {
		_, err = s.st.GetItem(id)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	runs, err := s.st.ListQARuns(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

// GET /api/qa-evidence/{run}/{attempt}/{file...}: one evidence file of a run, behind the session guard.
// attempt 0 means the run folder itself (repos.json). Only the exact patterns qa.ValidEvidenceFile accepts are served.
func (s *server) getQAEvidence(w http.ResponseWriter, r *http.Request) {
	run, err1 := strconv.ParseInt(r.PathValue("run"), 10, 64)
	attempt, err2 := strconv.Atoi(r.PathValue("attempt"))
	file := r.PathValue("file")
	if err1 != nil || err2 != nil || attempt < 0 || !qa.ValidEvidenceFile(file) || strings.Contains(file, "..") {
		writeErr(w, store.ErrNotFound)
		return
	}
	if _, err := s.st.GetQARun(run); err != nil {
		writeErr(w, store.ErrNotFound)
		return
	}
	p := s.qaPaths()
	dir := p.Run(run)
	if attempt > 0 {
		dir = p.Attempt(run, attempt)
	}
	full := filepath.Join(dir, filepath.FromSlash(file))
	real, err := filepath.EvalSymlinks(full)
	root, _ := filepath.EvalSymlinks(p.Run(run))
	if err != nil || root == "" || !strings.HasPrefix(real, root+string(filepath.Separator)) {
		writeErr(w, store.ErrNotFound)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if strings.HasSuffix(file, ".zip") || strings.HasSuffix(file, ".webm") {
		w.Header().Set("Content-Disposition", "attachment")
	}
	http.ServeFile(w, r, real)
}

var _ = json.Marshal
var _ = errors.New
