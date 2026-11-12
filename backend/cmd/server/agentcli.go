package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/arcane-sn/one-piece/backend/internal/gitx"
	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// agentCommands are the subcommands Claude may run during a brainstorming session.
// The server gives it the database path and board id through the environment, so Claude
// cannot point them elsewhere; every write goes through store's Draft guards.
var agentCommands = map[string]bool{"tickets": true, "ticket": true, "draft-add": true, "draft-update": true,
	"branches": true, "branch-plan": true, "branch-status": true, "branch-pr": true, "git": true, "gh": true,
	"ticket-show": true, "ticket-edit": true, "flow-add": true, "flow-update": true, "flow-delete": true, "review-verdict": true}

// reviewCommands only work in a Claude Review session (ONE_PIECE_MODE=review, ONE_PIECE_CARD set):
// they read and edit THAT ticket and its Business Flows, and record the verdict.
var reviewCommands = map[string]bool{"ticket-show": true, "ticket-edit": true, "flow-add": true, "flow-update": true, "flow-delete": true, "review-verdict": true}

// branchCommands need ONE_PIECE_CARD: the ticket a Discuss session is about.
var branchCommands = map[string]bool{"branches": true, "branch-plan": true, "branch-status": true, "branch-pr": true, "git": true, "gh": true}

// runAgentCLI runs one agent subcommand (args[0] is its name).
//
//	tickets                      one line per ticket: #id status title
//	ticket <id>                  one ticket in full
//	draft-add <title>            new Draft, description on stdin
//	draft-update <id> <title>    change a Draft, description on stdin
//
// Discuss sessions (ONE_PIECE_CARD set) also get, for that ticket only:
//
//	branches                          the ticket's branching rows
//	branch-plan <repo> <base>         record repo + base branch (names come from the board's prefix)
//	branch-status <repo> <status> [note…]
//	branch-pr <repo> <url>            store the PR link, status pr_open
//	git <repo> <args…>, gh <repo> <args…>   guarded by internal/gitx; repo must be registered
func runAgentCLI(args []string, getenv func(string) string, in io.Reader, out io.Writer) error {
	dbPath := getenv("ONE_PIECE_DB")
	boardID, err := strconv.ParseInt(getenv("ONE_PIECE_BOARD"), 10, 64)
	if dbPath == "" || err != nil || boardID <= 0 {
		return errors.New("ONE_PIECE_DB and ONE_PIECE_BOARD must be set")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.DB.Close()

	id := func(i int) (int64, error) {
		if len(args) <= i {
			return 0, fmt.Errorf("usage: %s <id> ...", args[0])
		}
		return strconv.ParseInt(args[i], 10, 64)
	}
	description := func() (string, error) {
		b, err := io.ReadAll(io.LimitReader(in, 1<<20))
		return strings.TrimSpace(string(b)), err
	}

	var card store.Card
	if reviewCommands[args[0]] {
		if getenv("ONE_PIECE_MODE") != "review" {
			return errors.New(args[0] + " is only available in a Claude Review session")
		}
		cid, err := strconv.ParseInt(getenv("ONE_PIECE_CARD"), 10, 64)
		if err != nil || cid <= 0 {
			return errors.New("ONE_PIECE_CARD must be set for " + args[0])
		}
		if card, err = st.GetCard(cid); err != nil || card.BoardID != boardID {
			return errors.New("ticket not found")
		}
	}
	if branchCommands[args[0]] {
		cid, err := strconv.ParseInt(getenv("ONE_PIECE_CARD"), 10, 64)
		if err != nil || cid <= 0 {
			return errors.New("ONE_PIECE_CARD must be set for " + args[0])
		}
		if card, err = st.GetCard(cid); err != nil || card.BoardID != boardID {
			return errors.New("ticket not found")
		}
	}
	// registered finds the ticket's branching row for an absolute repo path.
	registered := func(repo string) (store.Branch, error) {
		rows, err := st.ListBranches(card.ID)
		if err != nil {
			return store.Branch{}, err
		}
		for _, r := range rows {
			if filepath.IsAbs(repo) && r.Repo == repo {
				return r, nil
			}
		}
		return store.Branch{}, errors.New("repo is not registered for this ticket; run branch-plan first")
	}

	// ownFlow finds a Business Flow of THIS ticket by the id in args[1].
	ownFlow := func() (store.Flow, error) {
		fid, err := id(1)
		if err != nil {
			return store.Flow{}, err
		}
		f, err := st.GetFlow(fid)
		if err != nil || f.CardID != card.ID || f.ItemID != 0 {
			return store.Flow{}, errors.New("flow not found on this ticket")
		}
		return f, nil
	}
	event := func(text string, ref int64) { st.AddReviewMessage(card.ID, "event", text, "done", ref) }

	switch args[0] {
	case "ticket-show":
		fmt.Fprintf(out, "%s — %s\n[%s] content version %d\n\nDescription:\n%s\n", card.Ident, card.Title, card.Status, card.ContentVersion, card.Description)
		fl, err := st.ListFlows(card.ID)
		if err != nil {
			return err
		}
		if len(fl) == 0 {
			fmt.Fprintln(out, "\nBusiness Flows: none")
		}
		for _, f := range fl {
			fmt.Fprintf(out, "\nBusiness Flow %d | %s\n%s\n", f.ID, f.Title, f.Source)
		}
	case "ticket-edit":
		if len(args) != 2 {
			return errors.New("usage: ticket-edit <title>  (the whole new description on stdin)")
		}
		desc, err := description()
		if err != nil {
			return err
		}
		c, rev, err := st.ReviseCard(card.ID, args[1], desc)
		if err != nil {
			return err
		}
		if rev == 0 {
			fmt.Fprintln(out, "nothing changed")
			break
		}
		what := "Updated the ticket"
		if args[1] != card.Title {
			what += " (title and description)"
		} else {
			what += " description"
		}
		event(what, rev)
		fmt.Fprintf(out, "%s updated (content version %d)\n", c.Ident, c.ContentVersion)
	case "flow-add":
		if len(args) != 2 {
			return errors.New("usage: flow-add <title>  (swimlane text on stdin)")
		}
		src, err := description()
		if err != nil {
			return err
		}
		if err := store.CheckSwimlane(src); err != nil {
			return err
		}
		f, err := st.CreateFlow(card.ID, args[1], src)
		if err != nil {
			return err
		}
		event("Added Business Flow: "+f.Title, 0)
		fmt.Fprintf(out, "flow %d added\n", f.ID)
	case "flow-update":
		f, err := ownFlow()
		if err != nil || len(args) != 3 {
			return errors.New("usage: flow-update <id> <title>  (swimlane text on stdin)")
		}
		src, err := description()
		if err != nil {
			return err
		}
		if err := store.CheckSwimlane(src); err != nil {
			return err
		}
		if f, err = st.UpdateFlow(f.ID, args[2], src); err != nil {
			return err
		}
		event("Updated Business Flow: "+f.Title, 0)
		fmt.Fprintf(out, "flow %d updated\n", f.ID)
	case "flow-delete":
		f, err := ownFlow()
		if err != nil {
			return err
		}
		if err := st.DeleteFlow(f.ID); err != nil {
			return err
		}
		event("Removed Business Flow: "+f.Title, 0)
		fmt.Fprintf(out, "flow %d removed\n", f.ID)
	case "review-verdict":
		if len(args) < 2 {
			return errors.New("usage: review-verdict <pass|needs_work> [open point…]  (summary on stdin)")
		}
		summary, err := description()
		if err != nil {
			return err
		}
		r, err := st.RecordVerdict(card.ID, args[1], summary, args[2:])
		if err != nil {
			return err
		}
		if r.Verdict == "pass" {
			event("Verdict: Passed", 0)
		} else {
			event(fmt.Sprintf("Verdict: Needs Work (%d open)", len(r.Missing)), 0)
		}
		fmt.Fprintf(out, "verdict %s recorded for content version %d\n", r.Verdict, r.ContentVersion)
	case "branches":
		rows, err := st.ListBranches(card.ID)
		if err != nil {
			return err
		}
		for _, r := range rows {
			fmt.Fprintf(out, "%s | %s → %s | %s | %s | %s\n", r.Repo, r.BaseBranch, r.FeatureBranch, r.DevBranch, r.Status, r.PRURL)
		}
	case "branch-plan":
		if len(args) != 3 {
			return errors.New("usage: branch-plan <repo> <base-branch>")
		}
		r, err := st.PlanBranch(card.ID, args[1], args[2])
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s | %s → %s | %s | %s\n", r.Repo, r.BaseBranch, r.FeatureBranch, r.DevBranch, r.Status)
	case "branch-status":
		if len(args) < 3 {
			return errors.New("usage: branch-status <repo> <status> [note…]")
		}
		r, err := registered(args[1])
		if err != nil {
			return err
		}
		patch := store.BranchPatch{Status: &args[2]}
		if len(args) > 3 {
			note := strings.Join(args[3:], " ")
			patch.Note = &note
		}
		if r, err = st.UpdateBranch(card.ID, r.Repo, patch); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s is now %s\n", r.Repo, r.Status)
	case "branch-pr":
		if len(args) != 3 {
			return errors.New("usage: branch-pr <repo> <url>")
		}
		r, err := registered(args[1])
		if err != nil {
			return err
		}
		status := "pr_open"
		if r, err = st.UpdateBranch(card.ID, r.Repo, store.BranchPatch{PRURL: &args[2], Status: &status}); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: PR %s\n", r.Repo, r.PRURL)
	case "git", "gh":
		if len(args) < 3 {
			return fmt.Errorf("usage: %s <repo> <args…>", args[0])
		}
		r, err := registered(args[1])
		if err != nil {
			return err
		}
		board, err := st.GetBoard(boardID)
		if err != nil {
			return err
		}
		pol := gitx.Policy{Feature: r.FeatureBranch, Dev: r.DevBranch, Base: r.BaseBranch, PRBase: board.DevBranch}
		run := gitx.Git
		if args[0] == "gh" {
			run = gitx.GH
		}
		res, err := run(r.Repo, args[2:], pol)
		if res != "" {
			fmt.Fprintln(out, res)
		}
		if err != nil {
			return err
		}
	case "tickets":
		cs, err := st.ListCards(boardID)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, store.TicketIndex(cs))
	case "ticket":
		cid, err := id(1)
		if err != nil {
			return err
		}
		c, err := st.GetCard(cid)
		if err != nil || c.BoardID != boardID {
			return errors.New("ticket not found")
		}
		fmt.Fprintf(out, "#%d [%s] %s\n", c.ID, c.Status, c.Title)
		if c.Deadline != "" {
			fmt.Fprintf(out, "Deadline: %s\n", c.Deadline)
		}
		fmt.Fprintf(out, "\n%s\n", c.Description)
	case "draft-add":
		if len(args) != 2 {
			return errors.New("usage: draft-add <title>  (description on stdin)")
		}
		desc, err := description()
		if err != nil {
			return err
		}
		c, err := st.CreateDraft(boardID, args[1], desc)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "#%d created\n", c.ID)
	case "draft-update":
		cid, err := id(1)
		if err != nil || len(args) != 3 {
			return errors.New("usage: draft-update <id> <title>  (description on stdin)")
		}
		desc, err := description()
		if err != nil {
			return err
		}
		c, err := st.UpdateDraft(boardID, cid, args[2], desc)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "#%d updated\n", c.ID)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}
