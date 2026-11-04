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

const itemsSystemPrompt = `You break an approved Business ticket into work for a team of coding agents and QA.
Return ONLY one JSON object, no prose and no code fence:
{"split":false,"reason":"","agent_tasks":[{"title":"...","body":"Markdown description with the technical approach"}],"qa_cases":[{"title":"...","body":"Markdown: steps and expected result"}]}
Rules:
- Agent tasks: 2-6 small tasks, each one a focused change a single agent can finish; order them so earlier ones unblock later ones.
- QA cases: 3-8 test cases covering the Definition of Done, the failure paths and the Business Flow.
- If the ticket is too big for that (a whole application, several unrelated changes), return {"split":true,"reason":"what to split it into","agent_tasks":[],"qa_cases":[]}.
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
	if _, err := s.st.UpdateItem(id, in.Title, in.Body); err != nil {
		writeErr(w, err)
		return
	}
	if in.Status != "" && in.Status != old.Status {
		if _, err := s.st.SetItemStatus(id, in.Status); err != nil {
			writeErr(w, err)
			return
		}
		s.st.AppendItemLog(id, "Status: "+in.Status)
	}
	if strings.TrimSpace(in.Title) != old.Title || in.Body != old.Body {
		s.st.AppendItemLog(id, "Edited by you")
	}
	it, _ := s.st.GetItem(id)
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

// POST /api/cards/{id}/generate-items → {"split":bool,"reason":"","items":[...]}
// Only an approved ticket gets tasks. A "split" verdict changes nothing.
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
	if !c.Approved {
		writeErr(w, store.ConflictError{Msg: "Approve the ticket before generating tasks."})
		return
	}
	var msg strings.Builder
	if b, err := s.st.GetBoard(c.BoardID); err == nil {
		fmt.Fprintf(&msg, "Product: %s\n", b.Name)
		if b.Description != "" {
			fmt.Fprintf(&msg, "About the product: %s\n", b.Description)
		}
	}
	fmt.Fprintf(&msg, "\nTicket title: %s\n\nDescription (Markdown):\n%s\n", c.Title, c.Description)
	flows, _ := s.st.ListFlows(c.ID)
	for _, f := range flows {
		fmt.Fprintf(&msg, "\nBusiness Flow %q (swimlane text):\n%s\n", f.Title, f.Source)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	out, err := runClaude(ctx, itemsSystemPrompt, msg.String())
	var rep generatedItems
	if err == nil {
		a, b := strings.Index(out, "{"), strings.LastIndex(out, "}")
		if a < 0 || b < a {
			err = fmt.Errorf("no JSON in reply")
		} else {
			err = json.Unmarshal([]byte(out[a:b+1]), &rep)
		}
	}
	if err == nil && !rep.Split && len(rep.AgentTasks) == 0 {
		err = fmt.Errorf("reply has no agent tasks")
	}
	if err != nil {
		log.Printf("generate-items for card %d failed: %v", id, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Claude could not generate tasks. Try again."})
		return
	}
	if rep.Split {
		writeJSON(w, http.StatusOK, map[string]any{"split": true, "reason": strings.TrimSpace(rep.Reason), "items": []store.Item{}})
		return
	}
	logLine := []string{fmt.Sprintf("Generated by Claude from ticket version %d", c.ContentVersion)}
	var ins []store.Item
	for _, t := range rep.AgentTasks {
		ins = append(ins, store.Item{Kind: "agent", Title: t.Title, Body: t.Body, Log: logLine})
	}
	for _, t := range rep.QACases {
		ins = append(ins, store.Item{Kind: "qa", Title: t.Title, Body: t.Body, Log: logLine})
	}
	items, err := s.st.ReplaceItems(id, ins)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"split": false, "reason": "", "items": items})
}
