package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// swimlaneRules teaches Claude the swimlane text format the app draws (see frontend/src/lib/swimlanes.js).
const swimlaneRules = `Write the flow in this swimlane text format, one statement per line:
- First line: "title: <short flow name>". Second line: "autonumber".
- A step is "Sender -> Receiver: message". Participants are plain names (letters, digits, spaces, no colons or arrows); each becomes a lane, in order of first appearance. Use at most 6 participants.
- Arrows: "->" a call or action, "-->" a reply or return (dashed), "->>" an async call, "-x" a rejected or failed call. Always put "Sender", the arrow, "Receiver", then ": message" with spaces around the arrow.
- A participant can message itself: "API -> API: validate the token".
- Branches: "if: <condition>", the steps indented by two spaces, optional "else: <condition>" with its steps indented, then "end".
- Section label: "_: <text>". A side note: "note: <text>" on its own line.
- Message text is short (under about 60 characters). Inline code uses backticks, bold uses **double stars**.
- 6-15 steps in total. No blank lines inside an if/else block. No other syntax.
Example:
title: Owner signs in
autonumber
Owner -> App: Opens the login page
Owner -> App: Submits username and password
App -> Server: Verify credentials
if: credentials are valid
  Server --> App: Session cookie
  App --> Owner: Shows the dashboard
else: credentials are wrong
  Server -x App: 401
  App --> Owner: Shows an error
end`

const businessFlowPrompt = `You draw a Business Flow for a product ticket: who does what, in order, from the point of view of the people and teams involved (customer, support, operations, the system as one box).
Keep it about the business, not the code: no function names, tables or HTTP details.
` + swimlaneRules + `
Return ONLY one JSON object, no prose and no code fence: {"title":"short flow name","source":"the swimlane text"}
Write in the same language as the ticket and the request.`

const technicalFlowPrompt = `You draw a Technical Flow for one Agent Task: how the parts of the system talk to each other to implement it (screens, components, services, API, database).
Participants are technical parts, messages name the real action (endpoint, function, query) when the task gives one.
` + swimlaneRules + `
Return ONLY one JSON object, no prose and no code fence: {"title":"short flow name","source":"the swimlane text"}
Write in the same language as the task and the request.`

var flowArrow = regexp.MustCompile(`\S\s+(<->|-->>|->>|-->|<--|->|<-|-x|=>|<=)\s+\S`)

type genFlowIn struct {
	Kind     string `json:"kind"` // business | technical
	ID       int64  `json:"id"`   // card id (business) or Agent Task id (technical)
	Prompt   string `json:"prompt"`
	Previous string `json:"previous"` // an earlier draft whose lines the drawing could not read
	Problems []int  `json:"problems"` // 1-based line numbers of that draft that were not understood
}

// POST /api/flows/generate → 202 + a job whose result is {"title","source"}: a draft flow in swimlane text, not saved yet.
func (s *server) generateFlow(w http.ResponseWriter, r *http.Request) {
	var in genFlowIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	if n := utf8.RuneCountInString(in.Prompt); n < 1 || n > 20000 {
		writeErr(w, store.ValidationError{Msg: "prompt must be 1-20000 characters"})
		return
	}
	if utf8.RuneCountInString(in.Previous) > 10000 || len(in.Problems) > 200 {
		writeErr(w, store.ValidationError{Msg: "previous draft is too long"})
		return
	}
	var card store.Card
	var item store.Item
	var err error
	system := businessFlowPrompt
	switch in.Kind {
	case "business":
		card, err = s.st.GetCard(in.ID)
	case "technical":
		system = technicalFlowPrompt
		if item, err = s.st.GetItem(in.ID); err == nil {
			card, err = s.st.GetCard(item.CardID)
		}
	default:
		err = store.ValidationError{Msg: "kind must be business or technical"}
	}
	if err != nil {
		writeErr(w, err)
		return
	}

	var msg strings.Builder
	if b, err := s.st.GetBoard(card.BoardID); err == nil {
		fmt.Fprintf(&msg, "Product: %s\n", b.Name)
		if b.Description != "" {
			fmt.Fprintf(&msg, "About the product: %s\n", b.Description)
		}
	}
	fmt.Fprintf(&msg, "\nTicket ID: %s\nTicket title: %s\n\nTicket description (Markdown):\n%s\n", card.Ident, card.Title, card.Description)
	if in.Kind == "technical" {
		fmt.Fprintf(&msg, "\nAgent Task: %s\n%s\n", item.Title, item.Body)
		if fl, _ := s.st.ListFlows(card.ID); len(fl) > 0 {
			for _, f := range fl {
				fmt.Fprintf(&msg, "\nBusiness Flow %q (swimlane text):\n%s\n", f.Title, f.Source)
			}
		}
	}
	fmt.Fprintf(&msg, "\nWhat the owner wants drawn:\n%s\n", in.Prompt)
	if strings.TrimSpace(in.Previous) != "" && len(in.Problems) > 0 {
		fmt.Fprintf(&msg, "\nYour previous draft had lines the drawing could not read (line numbers %v). Fix them, keep the rest:\n%s\n", in.Problems, in.Previous)
	}

	imgs := s.attachedImages(in.Prompt)
	imgNote := withImages(&msg, imgs)
	scope, target, label := "card", card.ID, "Business Flow "+card.Ident
	if in.Kind == "technical" {
		scope, target, label = "item", item.ID, "Technical Flow "+item.Ident
	}
	reqJSON, _ := json.Marshal(map[string]any{"prompt": in.Prompt, "retry": in.Previous != ""}) // retry = Claude's second go at unreadable lines
	s.startJob(w, store.Job{Kind: "flow", Scope: scope, TargetID: target, CardID: card.ID, Label: label, Request: string(reqJSON)}, 3*time.Minute,
		func(ctx context.Context) (any, bool, error) {
			var out string
			var err error
			if len(imgs) > 0 {
				out, err = streamClaude(ctx, system+imgNote, msg.String(), []string{s.uploadsDir()}, func(string) {})
			} else {
				out, err = runClaude(ctx, system, msg.String())
			}
			title, source := parseFlowReply(out)
			if err != nil || !flowArrow.MatchString(source) {
				log.Printf("generate-flow failed: err=%v source=%q", err, shorten(source, 80))
				return nil, false, fmt.Errorf("Claude could not draw that flow. Try again.")
			}
			return map[string]string{"title": title, "source": source}, false, nil
		})
}

// parseFlowReply reads {"title","source"} from Claude's reply, tolerating a code fence or bare swimlane text.
func parseFlowReply(out string) (title, source string) {
	if a, z := strings.Index(out, "{"), strings.LastIndex(out, "}"); a >= 0 && z > a {
		var v struct{ Title, Source string }
		if json.Unmarshal([]byte(out[a:z+1]), &v) == nil {
			title, source = v.Title, v.Source
		}
	}
	if source == "" { // bare text, maybe fenced
		source = strings.TrimSpace(out)
		source = strings.TrimSuffix(strings.TrimPrefix(source, "```"), "```")
		source = strings.TrimSpace(strings.TrimPrefix(source, "swimlane"))
	}
	source = strings.TrimSpace(source)
	lines := strings.Split(source, "\n")
	if title == "" && len(lines) > 0 && strings.HasPrefix(strings.ToLower(lines[0]), "title:") {
		title = strings.TrimSpace(lines[0][len("title:"):])
	}
	if title = strings.TrimSpace(title); utf8.RuneCountInString(title) > 200 {
		title = string([]rune(title)[:200])
	}
	return title, source + "\n"
}

const autoFlowNote = `
You may read the product's code (Read, Grep, Glob; a handful of tool calls). Name the real files, components, functions and endpoints this task touches or calls, and make the lanes those files or modules; never invent a name you did not see. If the code is not there yet (a new file), say so in the message, e.g. "(new) useActivityLog.ts".`

// autoTechnicalFlow drafts the Technical Flow of a freshly generated Agent Task from the code, so every task starts with
// one. A failure only means the task has no flow yet; the owner can still press Generate in the task.
func (s *server) autoTechnicalFlow(ctx context.Context, card store.Card, it store.Item, b store.Board) error {
	var msg strings.Builder
	fmt.Fprintf(&msg, "Product: %s\n", b.Name)
	if b.Description != "" {
		fmt.Fprintf(&msg, "About the product: %s\n", b.Description)
	}
	fmt.Fprintf(&msg, "\nTicket ID: %s\nTicket title: %s\n\nTicket description (Markdown):\n%s\n", card.Ident, card.Title, card.Description)
	fmt.Fprintf(&msg, "\nAgent Task: %s\n%s\n", it.Title, it.Body)
	if fl, _ := s.st.ListFlows(card.ID); len(fl) > 0 {
		for _, f := range fl {
			fmt.Fprintf(&msg, "\nBusiness Flow %q (swimlane text):\n%s\n", f.Title, f.Source)
		}
	}
	msg.WriteString("\nWhat the owner wants drawn:\nThe technical flow of this task between the real files and functions that implement it.\n")
	dirs := b.Folders
	if repos, _ := s.ticketRepos(card.ID); len(repos) > 0 {
		dirs = nil
		for _, r := range repos {
			dirs = append(dirs, r.Repo)
		}
	}
	out, err := streamClaude(ctx, technicalFlowPrompt+autoFlowNote, msg.String(), dirs, func(string) {})
	title, source := parseFlowReply(out)
	if err != nil || !flowArrow.MatchString(source) {
		return fmt.Errorf("no readable flow (err=%v)", err)
	}
	if title == "" {
		title = "Technical Flow"
	}
	_, err = s.st.CreateItemFlow(it.ID, title, source)
	return err
}

// autoFlows runs autoTechnicalFlow for each task, three at a time.
func (s *server) autoFlows(ctx context.Context, card store.Card, items []store.Item) (made int) {
	b, err := s.st.GetBoard(card.BoardID)
	if err != nil {
		return 0
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	for _, it := range items {
		wg.Add(1)
		go func(it store.Item) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			if err := s.autoTechnicalFlow(ctx, card, it, b); err != nil {
				log.Printf("auto flow for %s: %v", it.Ident, err)
				return
			}
			mu.Lock()
			made++
			mu.Unlock()
		}(it)
	}
	wg.Wait()
	return made
}
