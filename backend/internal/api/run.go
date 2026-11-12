package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

const runSystemPrompt = `You are the implementing engineer on one Agent Task of a Business ticket.
Work in the current directory (the product's main folder); other product folders may be added as extra directories.
Do only this task. Do not touch anything the task does not need, do not commit, and do not run shell commands.
Read the relevant code first, follow the existing style, then make the edits.
Your final message is a short summary (2-4 sentences) of what you changed and anything left undone. Write in the same language as the task.`

const qaSystemPrompt = `You are a QA engineer checking one test case against the implemented code.
You may only read code (Read, Grep, Glob) in the listed product folders; you cannot run the app or any command, so judge by tracing the code and say so when something can only be confirmed by running it.
Check the test case's steps and expected result against what the code does now.
Your final message must be ONLY one JSON object and nothing else:
{"result":"pass"|"fail","issue":"one sentence (max 140 characters) naming what is wrong; empty with pass","desc":"with fail: what the code does now, what the test case expects, and the files and functions involved, so a developer can fix it; with pass: one or two sentences of evidence"}
Use "pass" only when the code clearly satisfies the expected result; otherwise "fail". Write in the same language as the test case.`

// agentRun runs `claude -p` with edit tools in the product's first folder (cwd) and
// the remaining folders as extra directories. It is a variable so tests can stub it.
var agentRun = func(ctx context.Context, cwd string, dirs []string, system, user string, onLog func(string)) (string, error) {
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--no-session-persistence",
		"--setting-sources", "project", "--permission-mode", "acceptEdits",
		"--allowedTools", "Read,Grep,Glob,Edit,Write", "--system-prompt", system}
	for _, d := range dirs {
		args = append(args, "--add-dir", d)
	}
	return runStream(ctx, args, cwd, user, onLog)
}

// POST /api/items/{id}/run → start Claude on an Agent Task; progress lands in the item's log.
func (s *server) runItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	it, err := s.st.GetItem(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	c, b, err := s.runnable(it, true)
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, it, err := s.claimItem(context.Background(), it, "Run started by you")
	if err != nil {
		writeErr(w, err)
		return
	}
	go s.executeItem(ctx, it, c, b)
	it.Running = true
	writeJSON(w, http.StatusAccepted, it)
}

// runnable checks an item may be run now and returns its card and board. gate=false skips the
// "all Agent Tasks done" rule for QA, which a chain checks once at its start (a failing test adds [FIX] tasks).
func (s *server) runnable(it store.Item, gate bool) (store.Card, store.Board, error) {
	c, err := s.st.GetCard(it.CardID)
	if err != nil {
		return c, store.Board{}, err
	}
	switch {
	case !c.Approved:
		err = store.ConflictError{Msg: "Approve the ticket before running its tasks."}
	case it.Status == "done":
		err = store.ConflictError{Msg: "This item is already done. Set it back to To Do to run it again."}
	}
	if err == nil && gate && it.Kind == "qa" {
		all, lerr := s.st.ListItems(it.CardID)
		if lerr != nil {
			return c, store.Board{}, lerr
		}
		open := 0
		for _, x := range all {
			if x.Kind == "agent" && x.Status != "done" {
				open++
			}
		}
		if open > 0 {
			err = store.ConflictError{Msg: fmt.Sprintf("Finish all Agent Tasks before running QA Test Cases (%d not done yet, including any [FIX] tasks).", open)}
		}
	}
	if err != nil {
		return c, store.Board{}, err
	}
	b, err := s.st.GetBoard(c.BoardID)
	if err != nil {
		return c, b, err
	}
	if len(b.Folders) == 0 {
		return c, b, store.ConflictError{Msg: "This board has no folder for Claude to work in."}
	}
	if fi, err := os.Stat(b.Folders[0]); err != nil || !fi.IsDir() {
		return c, b, store.ConflictError{Msg: "Folder not found: " + b.Folders[0]}
	}
	return c, b, nil
}

// claimItem registers a run for the item (one at a time) and marks it Doing.
func (s *server) claimItem(parent context.Context, it store.Item, first string) (context.Context, store.Item, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	s.mu.Lock()
	if _, busy := s.runs[it.ID]; busy {
		s.mu.Unlock()
		cancel()
		return nil, it, store.ConflictError{Msg: "This task is already running."}
	}
	s.runs[it.ID] = cancel
	s.mu.Unlock()
	it, err := s.st.SaveItem(it.ID, it.Title, it.Body, "doing", []string{first})
	if err != nil {
		s.finishRun(it.ID)
		return nil, it, err
	}
	return ctx, it, nil
}

// POST /api/cards/{id}/run-all → run the ticket's To Do Agent Tasks one after another;
// the chain stops at the first task that fails (it goes back to To Do with the reason in its log).
func (s *server) runAll(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Kind string `json:"kind"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if in.Kind == "" {
		in.Kind = "agent"
	}
	if in.Kind != "agent" && in.Kind != "qa" {
		writeErr(w, store.ValidationError{Msg: "kind must be agent or qa"})
		return
	}
	items, err := s.st.ListItems(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	var todo []store.Item
	for _, it := range items {
		if it.Kind == in.Kind && it.Status == "todo" {
			todo = append(todo, it)
		}
	}
	if len(todo) == 0 {
		writeErr(w, store.ConflictError{Msg: "Nothing in To Do to run."})
		return
	}
	if _, _, err := s.runnable(todo[0], true); err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if _, busy := s.chains[id]; busy {
		s.mu.Unlock()
		cancel()
		writeErr(w, store.ConflictError{Msg: "This ticket is already running."})
		return
	}
	s.chains[id] = cancel
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.chains, id)
			s.mu.Unlock()
			cancel()
		}()
		for _, it := range todo {
			if ctx.Err() != nil {
				return
			}
			c, b, err := s.runnable(it, false)
			if err != nil {
				s.st.AppendItemLog(it.ID, "Blocked: "+err.Error())
				return
			}
			rctx, it, err := s.claimItem(ctx, it, "Run started (Run All)")
			if err != nil {
				s.st.AppendItemLog(it.ID, "Blocked: "+err.Error())
				return
			}
			// a failed QA verdict is a finding, so the other test cases still run;
			// anything else that fails is a block and stops the chain
			if o := s.executeItem(rctx, it, c, b); o == runBlocked || (o == runFailed && it.Kind != "qa") {
				return
			}
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]int{"queued": len(todo)})
}

// POST /api/cards/{id}/stop-all → cancel the chain and the task currently running.
func (s *server) stopAll(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.mu.Lock()
	if cancel, ok := s.chains[id]; ok {
		cancel()
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) finishRun(id int64) {
	s.mu.Lock()
	if cancel, ok := s.runs[id]; ok {
		cancel()
		delete(s.runs, id)
	}
	s.mu.Unlock()
}

type runOutcome int

const (
	runDone    runOutcome = iota
	runFailed             // QA verdict "fail", or an Agent Task Claude could not finish
	runBlocked            // technical error, timeout or stop
)

// executeItem runs one item to completion: an Agent Task edits files, a QA Test Case
// is judged by reading the code.
func (s *server) executeItem(ctx context.Context, it store.Item, c store.Card, b store.Board) runOutcome {
	defer s.finishRun(it.ID)
	var msg strings.Builder
	fmt.Fprintf(&msg, "Product: %s\n", b.Name)
	if b.Description != "" {
		fmt.Fprintf(&msg, "About the product: %s\n", b.Description)
	}
	fmt.Fprintf(&msg, "\nBusiness ticket: %s\n%s\n", c.Title, c.Description)
	if it.Kind == "qa" {
		fmt.Fprintf(&msg, "\nYour QA Test Case: %s\n%s\n", it.Title, it.Body)
		fmt.Fprintf(&msg, "\nProduct folders you may read:\n- %s\n", strings.Join(b.Folders, "\n- "))
	} else {
		fmt.Fprintf(&msg, "\nYour Agent Task: %s\n%s\n", it.Title, it.Body)
		if fl, _ := s.st.ListItemFlows(it.ID); len(fl) > 0 {
			for _, f := range fl {
				fmt.Fprintf(&msg, "\nTechnical Flow \"%s\":\n%s\n", f.Title, f.Source)
			}
		}
	}
	logLine := func(line string) { s.st.AppendItemLog(it.ID, line) }
	var final string
	var err error
	if it.Kind == "qa" {
		final, err = streamClaude(ctx, qaSystemPrompt, msg.String(), b.Folders, logLine)
	} else {
		final, err = agentRun(ctx, b.Folders[0], b.Folders[1:], runSystemPrompt, msg.String(), logLine)
	}
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		logLine("Stopped")
		s.st.SetItemStatus(it.ID, "todo")
		return runBlocked
	case err != nil:
		log.Printf("run item %d failed: %v", it.ID, err)
		logLine("Failed: " + shorten(err.Error(), 300))
		s.st.SetItemStatus(it.ID, "todo")
		return runBlocked
	}
	if it.Kind == "qa" {
		res, issue, desc, perr := parseQA(final)
		switch {
		case perr != nil:
			logLine("Failed: Claude's answer could not be read. Try again.")
			s.st.SetItemStatus(it.ID, "todo")
			return runBlocked
		case res != "pass":
			logLine("Failed: " + issue)
			s.st.SetItemFailure(it.ID, issue, desc)
			if fix, made, err := s.st.AddFixTask(it, issue, desc); err != nil {
				logLine("Could not create a [FIX] task: " + err.Error())
			} else if made {
				logLine("Created Agent Task: " + fix.Title)
			}
			s.st.SetItemStatus(it.ID, "todo")
			return runFailed
		}
		s.st.SetItemFailure(it.ID, "", "")
		logLine("Passed: " + shorten(desc, 300))
		s.st.SetItemStatus(it.ID, "done")
		return runDone
	}
	logLine("Done: " + shorten(final, 400))
	s.st.SetItemStatus(it.ID, "done")
	return runDone
}

func parseQA(text string) (result, issue, desc string, err error) {
	a, z := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if a < 0 || z < a {
		return "", "", "", errors.New("no JSON object")
	}
	var v struct {
		Result string `json:"result"`
		Issue  string `json:"issue"`
		Desc   string `json:"desc"`
	}
	if err := json.Unmarshal([]byte(text[a:z+1]), &v); err != nil {
		return "", "", "", err
	}
	if v.Result != "pass" && v.Result != "fail" {
		return "", "", "", fmt.Errorf("unknown result %q", v.Result)
	}
	issue, desc = shorten(v.Issue, 200), strings.TrimSpace(v.Desc)
	if v.Result == "fail" && issue == "" {
		issue = shorten(desc, 140)
	}
	if len([]rune(desc)) > 3000 {
		desc = string([]rune(desc)[:3000]) + "…"
	}
	return v.Result, issue, desc, nil
}

// POST /api/items/{id}/stop → cancel a running task, or reset a stuck "doing" one to To Do.
func (s *server) stopItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	it, err := s.st.GetItem(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.mu.Lock()
	cancel, running := s.runs[id]
	s.mu.Unlock()
	if running {
		cancel()
		writeJSON(w, http.StatusAccepted, it)
		return
	}
	if it.Status == "doing" {
		s.st.AppendItemLog(id, "Reset to To Do (no run in progress)")
		it, err = s.st.SetItemStatus(id, "todo")
		if err != nil {
			writeErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, it)
}
