package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

const itemsSystemPrompt = `You break a Business ticket, the plan the owner is about to approve, into work for a team of coding agents.
Return ONLY one JSON object, no prose and no code fence:
{"split":false,"reason":"","agent_tasks":[{"title":"...","body":"Markdown description with the technical approach"}]}
Rules:
- 2-6 small tasks, each one a focused change a single agent can finish; order them so earlier ones unblock later ones.
- Keep each title short (one line, under about 80 characters); put the detail in the body.
- If the ticket is too big for that (a whole application, several unrelated changes), return {"split":true,"reason":"what to split it into","agent_tasks":[]}.
Write in the same language as the ticket.`

const qaSystemPromptGen = `You write the QA test cases of a Business ticket. They are its Definition of Done and are written BEFORE the Agent Tasks run, so the code of this ticket does not exist yet.
Return ONLY one JSON object, no prose and no code fence:
{"qa_cases":[{"title":"...","body":"Markdown: prerequisites, steps and expected result"}]}
Rules:
- 3-8 test cases. Each one describes behaviour a person can observe, and names the requirement of the ticket it covers (first line of the body: "Requirement: ..."). Every requirement gets at least one case; cover the failure paths and the Business Flow too.
- Keep each title short (one line, under about 80 characters); put the rest in the body.
- You may read the product's code (Read, Grep, Glob; a handful of tool calls) to name the real screens, buttons, routes and fields that exist today. For anything this ticket will add, describe what the person sees in words; never invent selectors, routes or click paths for code that is not written yet.
- Start every body with a "## Prerequisites" section, one bullet each, or "- none" when nothing special is needed:
  - Login: the kind of account the case needs (for example "admin", "a user without a PIN"); say "no login" for public pages.
  - Services: any other application or API that must be running besides the product itself (name it and what for).
  - Data: records or settings that must exist before the steps (for example "a transaction in pending status").
  Then "## Steps" and "## Expected result".
- Each case must be checkable later, once the code exists, by reading it and by using the product.
Write in the same language as the ticket.`

type itemIn struct {
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Status string `json:"status"`
}

type generatedItems struct {
	Split      bool   `json:"split"`
	Reason     string `json:"reason"`
	AgentTasks []struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	} `json:"agent_tasks"`
	QACases []struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	} `json:"qa_cases"`
}

func (s *server) listItems(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.st.GetCard(id); err != nil {
		writeErr(w, err)
		return
	}
	items, err := s.st.ListItems(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.mu.Lock()
	for i := range items {
		_, items[i].Running = s.runs[items[i].ID]
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, items)
}

func (s *server) createItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in itemIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if !s.editContract(w, id) {
		return
	}
	it, err := s.st.CreateItem(id, in.Kind, in.Title, in.Body)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.contractChanged(id)
	s.st.AppendItemLog(it.ID, "Added by you")
	it, _ = s.st.GetItem(it.ID)
	writeJSON(w, http.StatusCreated, it)
}

func (s *server) updateItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in itemIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	old, err := s.st.GetItem(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if in.Status == "doing" {
		s.mu.Lock()
		_, live := s.runs[id]
		s.mu.Unlock()
		if !live { // "In Progress" is only true while Claude is on it; a stale form must not set it
			if old.Status == "doing" {
				in.Status = "todo"
			} else {
				in.Status = old.Status
			}
		}
	}
	var lines []string
	if in.Status != "" && in.Status != old.Status {
		lines = append(lines, "Status: "+in.Status)
	}
	edited := strings.TrimSpace(in.Title) != old.Title || in.Body != old.Body
	if edited {
		lines = append(lines, "Edited by you")
		if !s.editContract(w, old.CardID) { // a status change is run bookkeeping; only a text edit changes the contract
			return
		}
	}
	it, err := s.st.SaveItem(id, in.Title, in.Body, in.Status, lines)
	if err != nil {
		writeErr(w, err)
		return
	}
	if edited {
		s.contractChanged(old.CardID)
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *server) deleteItem(w http.ResponseWriter, r *http.Request) {
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
	if !s.editContract(w, it.CardID) {
		return
	}
	if err := s.st.DeleteItem(id); err != nil {
		writeErr(w, err)
		return
	}
	s.contractChanged(it.CardID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listItemFlows(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.st.GetItem(id); err != nil {
		writeErr(w, err)
		return
	}
	fs, err := s.st.ListItemFlows(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fs)
}

func (s *server) createItemFlow(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in flowIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	f, err := s.st.CreateItemFlow(id, in.Title, in.Source)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.st.AppendItemLog(id, "Technical flow added: "+f.Title)
	writeJSON(w, http.StatusCreated, f)
}

// POST /api/cards/{id}/generate-items {"kind":"agent"|"qa"} → 202 + the job; its result is {"split","reason","count"}
// Agent Tasks and QA Test Cases are both part of the contract the owner approves, so they are generated in Business.
// Runs as a background job. Old To Do items of that kind are struck through (not deleted); a "split" verdict changes nothing.
func (s *server) generateItems(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.st.GetCard(id)
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
	switch {
	case in.Kind != "agent" && in.Kind != "qa":
		err = store.ValidationError{Msg: "kind must be agent or qa"}
	case c.Status != "business":
		err = store.ConflictError{Msg: "Generate the plan while the ticket is in Business."}
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	system := itemsSystemPrompt
	var msg strings.Builder
	var codeDirs []string // where Claude may read the code (QA cases are grounded in it)
	if b, err := s.st.GetBoard(c.BoardID); err == nil {
		codeDirs = b.Folders
		fmt.Fprintf(&msg, "Product: %s\n", b.Name)
		if b.Description != "" {
			fmt.Fprintf(&msg, "About the product: %s\n", b.Description)
		}
	}
	fmt.Fprintf(&msg, "\nTicket ID: %s\nTicket title: %s\n\nDescription (Markdown):\n%s\n", c.Ident, c.Title, c.Description)
	flows, _ := s.st.ListFlows(c.ID)
	for _, f := range flows {
		fmt.Fprintf(&msg, "\nBusiness Flow %q (swimlane text):\n%s\n", f.Title, f.Source)
	}
	if in.Kind == "qa" {
		system = qaSystemPromptGen
		existing, _ := s.st.ListItems(id)
		for _, x := range existing {
			if x.Kind == "agent" {
				fmt.Fprintf(&msg, "\nAgent Task (%s): %s\n%s\n", x.Status, x.Title, x.Body)
			}
		}
	}

	label := "Agent Tasks " + c.Ident
	jkind := "agent_tasks"
	if in.Kind == "qa" {
		label, jkind = "QA Test Cases "+c.Ident, "qa_cases"
	}
	if repos, _ := s.ticketRepos(c.ID); len(repos) > 0 {
		codeDirs = nil
		for _, r := range repos {
			codeDirs = append(codeDirs, r.Repo)
		}
	}
	prompt, kind, version := msg.String(), in.Kind, c.ContentVersion
	timeout := 4 * time.Minute
	if in.Kind == "qa" {
		timeout = 8 * time.Minute // it reads the code first
	}
	if in.Kind == "agent" {
		timeout = 12 * time.Minute // plus a Technical Flow drafted from the code for each task
	}
	s.startJob(w, store.Job{Kind: jkind, Scope: "card", TargetID: id, CardID: id, Label: label}, timeout,
		func(ctx context.Context) (any, bool, error) {
			var out string
			var err error
			if kind == "qa" && len(codeDirs) > 0 {
				out, err = streamClaude(ctx, system, prompt, codeDirs, func(string) {})
			} else {
				out, err = runClaude(ctx, system, prompt)
			}
			var rep generatedItems
			if err == nil {
				a, b := strings.Index(out, "{"), strings.LastIndex(out, "}")
				if a < 0 || b < a {
					err = fmt.Errorf("no JSON in reply")
				} else {
					err = json.Unmarshal([]byte(out[a:b+1]), &rep)
				}
			}
			if err == nil && !rep.Split && ((kind == "agent" && len(rep.AgentTasks) == 0) || (kind == "qa" && len(rep.QACases) == 0)) {
				err = fmt.Errorf("reply has no %s items", kind)
			}
			if err != nil {
				log.Printf("generate-items for card %d failed: %v", id, err)
				return nil, false, fmt.Errorf("Claude could not generate that. Try again.")
			}
			if rep.Split {
				return map[string]any{"split": true, "reason": strings.TrimSpace(rep.Reason), "count": 0}, true, nil
			}
			logLine := []string{fmt.Sprintf("Generated by Claude from ticket version %d", version)}
			var ins []store.Item
			if kind == "agent" {
				for _, t := range rep.AgentTasks {
					ins = append(ins, store.Item{Title: t.Title, Body: t.Body, Log: logLine, Version: version})
				}
			} else {
				for _, t := range rep.QACases {
					ins = append(ins, store.Item{Title: t.Title, Body: t.Body, Log: logLine, Version: version})
				}
			}
			if cur, gerr := s.st.GetCard(id); gerr != nil || cur.Status != "business" {
				return nil, false, fmt.Errorf("The ticket left Business, so nothing was added.") // the contract is locked outside Business
			}
			items, err := s.st.SupersedeAndAdd(id, kind, ins)
			if err != nil {
				return nil, false, err
			}
			s.contractChanged(id)
			flows := 0
			if kind == "agent" {
				flows = s.autoFlows(ctx, c, items)
			}
			return map[string]any{"split": false, "reason": "", "count": len(items), "flows": flows}, true, nil
		})
}
