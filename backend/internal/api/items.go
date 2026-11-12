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

const itemsSystemPrompt = `You break an approved Business ticket into work for a team of coding agents.
Return ONLY one JSON object, no prose and no code fence:
{"split":false,"reason":"","agent_tasks":[{"title":"...","body":"Markdown description with the technical approach"}]}
Rules:
- 2-6 small tasks, each one a focused change a single agent can finish; order them so earlier ones unblock later ones.
- Keep each title short (one line, under about 80 characters); put the detail in the body.
- If the ticket is too big for that (a whole application, several unrelated changes), return {"split":true,"reason":"what to split it into","agent_tasks":[]}.
Write in the same language as the ticket.`

const qaSystemPromptGen = `You write QA test cases for a Business ticket whose Agent Tasks are done.
Return ONLY one JSON object, no prose and no code fence:
{"qa_cases":[{"title":"...","body":"Markdown: steps and expected result"}]}
Rules:
- 3-8 test cases covering the Definition of Done, the failure paths and the Business Flow.
- Keep each title short (one line, under about 80 characters); put steps and the expected result in the body.
- Each test case must be checkable by reading the code, plus what a person would see when using the product.
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
	it, err := s.st.CreateItem(id, in.Kind, in.Title, in.Body)
	if err != nil {
		writeErr(w, err)
		return
	}
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
	var lines []string
	if in.Status != "" && in.Status != old.Status {
		lines = append(lines, "Status: "+in.Status)
	}
	if strings.TrimSpace(in.Title) != old.Title || in.Body != old.Body {
		lines = append(lines, "Edited by you")
	}
	it, err := s.st.SaveItem(id, in.Title, in.Body, in.Status, lines)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *server) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.st.DeleteItem(id); err != nil {
		writeErr(w, err)
		return
	}
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

// POST /api/cards/{id}/generate-items {"kind":"agent"|"qa"} → {"split":bool,"reason":"","items":[...]}
// Agent Tasks come from an approved ticket; QA Test Cases only once the ticket is in the QA column.
// Only that kind is replaced. A "split" verdict changes nothing.
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
	case !c.Approved:
		err = store.ConflictError{Msg: "Approve the ticket before generating tasks."}
	case in.Kind == "qa" && c.Status != "qa" && c.Status != "done":
		err = store.ConflictError{Msg: "Move the ticket to QA before generating test cases."}
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	system := itemsSystemPrompt
	var msg strings.Builder
	if b, err := s.st.GetBoard(c.BoardID); err == nil {
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

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	out, err := runClaude(ctx, system, msg.String())
	var rep generatedItems
	if err == nil {
		a, b := strings.Index(out, "{"), strings.LastIndex(out, "}")
		if a < 0 || b < a {
			err = fmt.Errorf("no JSON in reply")
		} else {
			err = json.Unmarshal([]byte(out[a:b+1]), &rep)
		}
	}
	if err == nil && !rep.Split && ((in.Kind == "agent" && len(rep.AgentTasks) == 0) || (in.Kind == "qa" && len(rep.QACases) == 0)) {
		err = fmt.Errorf("reply has no %s items", in.Kind)
	}
	if err != nil {
		log.Printf("generate-items for card %d failed: %v", id, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Claude could not generate that. Try again."})
		return
	}
	if rep.Split {
		writeJSON(w, http.StatusOK, map[string]any{"split": true, "reason": strings.TrimSpace(rep.Reason), "items": []store.Item{}})
		return
	}
	logLine := []string{fmt.Sprintf("Generated by Claude from ticket version %d", c.ContentVersion)}
	var ins []store.Item
	if in.Kind == "agent" {
		for _, t := range rep.AgentTasks {
			ins = append(ins, store.Item{Title: t.Title, Body: t.Body, Log: logLine, Version: c.ContentVersion})
		}
	} else {
		for _, t := range rep.QACases {
			ins = append(ins, store.Item{Title: t.Title, Body: t.Body, Log: logLine, Version: c.ContentVersion})
		}
	}
	items, err := s.st.ReplaceItemsOfKind(id, in.Kind, ins)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"split": false, "reason": "", "items": items})
}
