package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

const brainstormTurnTimeout = 5 * time.Minute

// The system prompt is byte-identical on every turn so the prompt cache keeps hitting;
// per-turn facts (ticket changes) travel in the user message.
const brainstormSystemPrompt = `You are a planning partner for one product owner. You help them plan a whole product, one conversation per product (board). You plan; you never build or write code.
How to talk:
- Ask ONE question per turn. Start from what the owner wants and why, not from solutions. Offer a short menu only if they are stuck; if they ask you to guess, propose something concrete.
- Now and then play back what you understood in a few short sentences, mark anything you are guessing as "my guess", and end by asking what is wrong or missing.
- Keep replies short. Reply in the language the owner writes in.
Where things live (the first message of a session gives the exact paths):
- Tickets: the command given there lists them ("tickets") and shows one ("ticket <id>"). Read a ticket only when you need its details.
- Draft tickets: "draft-add '<title>'" and "draft-update <id> '<title>'" create and change Draft tickets; put the description on stdin with a shell heredoc. A Draft is a rough ticket: what and why in 2-6 lines, it may be incomplete. You cannot change any other ticket.
- The plan: one Markdown document at the plan path from the first message. Keep it current with Edit or Write: product overview, goals and non-goals, the ticket list (title, one-line summary, card id), open questions. You may write no other file.
- Product folders (read-only) can be searched with Read, Grep and Glob.`

// brainstormRun is everything one Claude turn needs.
type brainstormRun struct {
	System, Prompt, SessionID string
	Exe, DB, PlanPath         string // Exe: space-free path of the server binary; DB, PlanPath: absolute
	BoardID                   int64
	Dirs                      []string     // board folders, read-only
	OnSession                 func(string) // called once, as soon as the CLI announces its session id
}

// runBrainstorm runs one `claude -p` turn and returns the session id for --resume.
// It is a variable so tests can stub it.
//
// Verified against the real CLI (spike, 2026-11-04):
//   - the final "result" event carries session_id; --resume keeps the same id; a dead id exits 1 with
//     "No conversation found" and is_error=true
//   - Bash(<exe> cmd:*) only matches when <exe> has no spaces; a heredoc after the command still matches
//   - file permissions use "//<absolute path>" (so "Write(/" + absPath + ")"); other files are denied
//   - --allowedTools and --add-dir are variadic, so the prompt goes through stdin
var runBrainstorm = func(ctx context.Context, in brainstormRun, onText func(string)) (string, error) {
	planDir := filepath.Dir(in.PlanPath)
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return "", err
	}
	return runSession(ctx, sessionSpec{
		System: in.System, Prompt: in.Prompt, SessionID: in.SessionID,
		Dirs: append([]string{planDir}, in.Dirs...),
		AllowedTools: []string{"Read", "Grep", "Glob",
			"Bash(" + in.Exe + " tickets:*)", "Bash(" + in.Exe + " ticket:*)",
			"Bash(" + in.Exe + " draft-add:*)", "Bash(" + in.Exe + " draft-update:*)",
			"Write(/" + in.PlanPath + ")", "Edit(/" + in.PlanPath + ")"},
		Env:       []string{"ONE_PIECE_DB=" + in.DB, "ONE_PIECE_BOARD=" + strconv.FormatInt(in.BoardID, 10)},
		OnSession: in.OnSession,
	}, onText)
}

// sessionSpec is one resumable `claude -p` turn: the Brainstorming chat and the Discuss session share it.
type sessionSpec struct {
	System, Prompt, SessionID string
	Dirs                      []string     // --add-dir, readable
	AllowedTools              []string     // --allowedTools entries
	Env                       []string     // KEY=VALUE added to the process environment
	OnSession                 func(string) // called once, as soon as the CLI announces its session id
}

// runSession runs the turn in a temp cwd, streams assistant text blocks to onText, and returns the
// session id (for --resume). See runBrainstorm for what was verified against the real CLI.
func runSession(ctx context.Context, spec sessionSpec, onText func(string)) (string, error) {
	tmp, err := os.MkdirTemp("", "one-piece-session-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--max-turns", "12",
		"--setting-sources", "project", "--system-prompt", spec.System}
	if spec.SessionID != "" {
		args = append(args, "--resume", spec.SessionID)
	}
	for _, d := range spec.Dirs {
		args = append(args, "--add-dir", d)
	}
	if len(spec.AllowedTools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, spec.AllowedTools...)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.Stdin = strings.NewReader(spec.Prompt)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	var final, sessionID string
	var isErr, announced bool
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var ev struct {
			Type      string `json:"type"`
			IsError   bool   `json:"is_error"`
			Result    string `json:"result"`
			SessionID string `json:"session_id"`
			Message   struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.SessionID != "" && !announced && spec.OnSession != nil { // a stopped turn must still be resumable
			announced = true
			spec.OnSession(ev.SessionID)
		}
		switch ev.Type {
		case "assistant":
			for _, c := range ev.Message.Content {
				if t := strings.TrimSpace(c.Text); c.Type == "text" && t != "" {
					onText(t)
				}
			}
		case "result":
			final, isErr, sessionID = ev.Result, ev.IsError, ev.SessionID
		}
	}
	werr := cmd.Wait()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if isErr || werr != nil {
		return "", fmt.Errorf("claude: %v: %s", werr, shorten(strings.TrimSpace(stderr.String()+" "+final), 200))
	}
	return sessionID, nil
}

// agentExe returns a stable, space-free path that points at this server binary. Claude's Bash
// permission patterns break on spaces, and a resumed session remembers the path it was given.
func agentExe() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(os.TempDir(), "one-piece-agent")
	if strings.ContainsAny(dir, " \t") {
		return "", fmt.Errorf("temp dir %q contains spaces", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	link := filepath.Join(dir, "op")
	tmp := fmt.Sprintf("%s.%d", link, time.Now().UnixNano())
	if err := os.Symlink(self, tmp); err != nil {
		return "", err
	}
	return link, os.Rename(tmp, link) // rename is atomic: concurrent turns never see a missing link
}

// indexChange describes how the ticket index moved since the session last saw it; "" if it did not.
func indexChange(prev, cur string) string {
	if prev == cur {
		return ""
	}
	set := func(s string) map[string]bool {
		m := map[string]bool{}
		for _, l := range strings.Split(s, "\n") {
			if l != "" {
				m[l] = true
			}
		}
		return m
	}
	p, c := set(prev), set(cur)
	var out []string
	for _, l := range strings.Split(cur, "\n") {
		if l != "" && !p[l] {
			out = append(out, "+ "+l)
		}
	}
	for _, l := range strings.Split(prev, "\n") {
		if l != "" && !c[l] {
			out = append(out, "- "+l)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "Tickets changed since your last look:\n" + strings.Join(out, "\n")
}

// brainstormPrompt builds the user message: full context for a fresh session, only changes for a resumed one.
func brainstormPrompt(b store.Board, exe, planPath, index string, st store.BrainstormState, text string) string {
	var m strings.Builder
	if st.SessionID == "" {
		fmt.Fprintf(&m, "Board: %s\n", b.Name)
		if b.Description != "" {
			fmt.Fprintf(&m, "About the product: %s\n", b.Description)
		}
		if len(b.Folders) > 0 {
			fmt.Fprintf(&m, "Product folders you may read:\n- %s\n", strings.Join(b.Folders, "\n- "))
		}
		fmt.Fprintf(&m, "Command: %s (tickets | ticket <id> | draft-add <title> | draft-update <id> <title>)\n", exe)
		fmt.Fprintf(&m, "Plan document: %s (read it first if it exists)\n", planPath)
		if index == "" {
			m.WriteString("Tickets: none yet\n")
		} else {
			fmt.Fprintf(&m, "Tickets:\n%s\n", index)
		}
		m.WriteString("\n")
	} else if ch := indexChange(st.SentIndex, index); ch != "" {
		m.WriteString(ch + "\n\n")
	}
	m.WriteString(text)
	return m.String()
}

func (s *server) turnRunning(boardID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.turns[boardID]
	return ok
}

// stopTurn cancels a board's running turn, if any.
func (s *server) stopTurn(boardID int64) {
	s.mu.Lock()
	cancel := s.turns[boardID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// snapshotPlan saves the plan file as a new version when it differs from the latest version.
func (s *server) snapshotPlan(boardID int64) {
	b, err := os.ReadFile(s.st.PlanPath(boardID))
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return
	}
	if v, err := s.st.LatestPlanVersion(boardID); err == nil && v.Content == string(b) {
		return
	}
	if _, err := s.st.AddPlanVersion(boardID, string(b)); err != nil {
		log.Printf("saving plan version of board %d: %v", boardID, err)
	}
}

// runTurn runs one chat turn in the background and settles the assistant message, session state and plan.
func (s *server) runTurn(ctx context.Context, cancel context.CancelFunc, b store.Board, exe string, msgID int64, text string) {
	defer func() { // last: the board only reads as idle once everything is saved
		cancel()
		s.mu.Lock()
		delete(s.turns, b.ID)
		s.mu.Unlock()
	}()
	st, _ := s.st.GetBrainstormState(b.ID)
	cards, _ := s.st.ListCards(b.ID)
	index := store.TicketIndex(cards)
	run := brainstormRun{System: brainstormSystemPrompt, SessionID: st.SessionID, Exe: exe, DB: s.st.Path,
		PlanPath: s.st.PlanPath(b.ID), BoardID: b.ID, Dirs: b.Folders}
	run.Prompt = brainstormPrompt(b, exe, run.PlanPath, index, st, text)
	run.Dirs, run.Prompt = s.attachImages(text, run.Dirs, run.Prompt)

	wrote := false
	onText := func(t string) {
		wrote = true
		s.st.AppendBrainstormText(msgID, t)
	}
	var announced string // session id the CLI reported, even if the turn never finishes
	run.OnSession = func(id string) { announced = id }
	fresh := st.SessionID == ""
	sid, err := runBrainstorm(ctx, run, onText)
	// Only a dead session may restart, and only before anything happened: replaying a turn that already
	// wrote text (or created Drafts) would do its work twice.
	if err != nil && !fresh && !wrote && ctx.Err() == nil && strings.Contains(err.Error(), "No conversation found") {
		log.Printf("brainstorm board %d: resume failed (%v), starting a fresh session", b.ID, err)
		run.SessionID, fresh, announced = "", true, ""
		run.Prompt = brainstormPrompt(b, exe, run.PlanPath, index, store.BrainstormState{}, text)
		_, run.Prompt = s.attachImages(text, nil, run.Prompt) // run.Dirs already has the uploads folder
		sid, err = runBrainstorm(ctx, run, onText)
	}
	if _, gone := s.st.GetBoard(b.ID); gone != nil { // deleted while Claude was working
		return
	}
	status := "done"
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		status = "stopped"
	case err != nil:
		status = "failed"
		log.Printf("brainstorm board %d failed: %v", b.ID, err)
		if !wrote {
			msg := "Claude could not answer. Try again."
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				msg = "Claude took too long to answer. Try again."
			}
			onText(msg)
		}
	}
	if status == "done" { // Claude may have created Drafts itself: its own changes count as seen
		cards, _ = s.st.ListCards(b.ID)
		s.st.SetBrainstormState(b.ID, store.BrainstormState{SessionID: sid, SentIndex: store.TicketIndex(cards)})
	} else if fresh && announced != "" { // a stopped/failed first turn: keep its session so the chat keeps its context
		s.st.SetBrainstormState(b.ID, store.BrainstormState{SessionID: announced, SentIndex: index})
	}
	s.snapshotPlan(b.ID)
	s.st.FinishBrainstormMessage(msgID, status)
}

type brainstormView struct {
	Messages []store.BrainstormMessage `json:"messages"`
	Running  bool                      `json:"running"`
	Plan     *store.PlanVersion        `json:"plan"`
	Versions []store.PlanVersion       `json:"versions"`
}

// GET /api/boards/{id}/brainstorm
func (s *server) getBrainstorm(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.st.GetBoard(id); err != nil {
		writeErr(w, err)
		return
	}
	v := brainstormView{Running: s.turnRunning(id)}
	if v.Messages, err = s.st.ListBrainstormMessages(id); err != nil {
		writeErr(w, err)
		return
	}
	if v.Versions, err = s.st.ListPlanVersions(id); err != nil {
		writeErr(w, err)
		return
	}
	if p, err := s.st.LatestPlanVersion(id); err == nil {
		v.Plan = &p
	} else if !errors.Is(err, store.ErrNotFound) {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// POST /api/boards/{id}/brainstorm/messages {"text": "..."} → 202; the reply arrives through GET.
func (s *server) postBrainstormMessage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" || len([]rune(text)) > 20000 {
		writeErr(w, store.ValidationError{Msg: "message must be 1-20000 characters"})
		return
	}
	b, err := s.st.GetBoard(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	exe, err := agentExe()
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), brainstormTurnTimeout)
	s.mu.Lock()
	if _, busy := s.turns[id]; busy {
		s.mu.Unlock()
		cancel()
		writeErr(w, store.ConflictError{Msg: "Claude is still answering. Stop it or wait."})
		return
	}
	s.turns[id] = cancel
	s.mu.Unlock()
	release := func() {
		cancel()
		s.mu.Lock()
		delete(s.turns, id)
		s.mu.Unlock()
	}
	if _, err := s.st.AddBrainstormMessage(id, "user", text, "done"); err != nil {
		release()
		writeErr(w, err)
		return
	}
	am, err := s.st.AddBrainstormMessage(id, "assistant", "", "running")
	if err != nil {
		release()
		writeErr(w, err)
		return
	}
	go s.runTurn(ctx, cancel, b, exe, am.ID, text)
	writeJSON(w, http.StatusAccepted, map[string]bool{"running": true})
}

// POST /api/boards/{id}/brainstorm/stop
func (s *server) stopBrainstorm(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.stopTurn(id)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/boards/{id}/brainstorm/new: forget the Claude session; transcript and plan stay.
func (s *server) newBrainstorm(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.st.GetBoard(id); err != nil {
		writeErr(w, err)
		return
	}
	if s.turnRunning(id) {
		writeErr(w, store.ConflictError{Msg: "Claude is still answering. Stop it or wait."})
		return
	}
	if err := s.st.SetBrainstormState(id, store.BrainstormState{}); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func planVersionArgs(r *http.Request) (int64, int, error) {
	id, err := pathID(r)
	if err != nil {
		return 0, 0, err
	}
	v, err := strconv.Atoi(r.PathValue("v"))
	if err != nil {
		return 0, 0, store.ErrNotFound
	}
	return id, v, nil
}

// GET /api/boards/{id}/plan/{v}
func (s *server) getPlanVersion(w http.ResponseWriter, r *http.Request) {
	id, v, err := planVersionArgs(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	pv, err := s.st.GetPlanVersion(id, v)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

// POST /api/boards/{id}/plan/{v}/restore: put an old version back as the newest one.
func (s *server) restorePlan(w http.ResponseWriter, r *http.Request) {
	id, v, err := planVersionArgs(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if s.turnRunning(id) {
		writeErr(w, store.ConflictError{Msg: "Claude is still answering. Stop it or wait."})
		return
	}
	old, err := s.st.GetPlanVersion(id, v)
	if err != nil {
		writeErr(w, err)
		return
	}
	path := s.st.PlanPath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		writeErr(w, err)
		return
	}
	if err := os.WriteFile(path, []byte(old.Content), 0o644); err != nil {
		writeErr(w, err)
		return
	}
	nv, err := s.st.AddPlanVersion(id, old.Content)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nv)
}
