package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/arcane-sn/one-piece/backend/internal/gitx"
	"github.com/arcane-sn/one-piece/backend/internal/store"
)

type shipResult struct {
	Repo   string `json:"repo"`
	Status string `json:"status"`          // pushed | pr_open | skipped | failed
	PRURL  string `json:"prUrl,omitempty"` //
	Note   string `json:"note"`
}

// POST /api/cards/{id}/ship pushes the branches of a finished ticket and opens its pull requests, through the
// same git guard Claude's closeout uses. Direct repos push the feature branch; via -dev repos must already be
// integrated (merged_dev, done in the Discuss closeout) and push the feature and -dev branch.
func (s *server) shipCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	card, err := s.st.GetCard(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if card.Status != "done" {
		writeErr(w, store.ConflictError{Msg: "Push and PR is available once the ticket is Done."})
		return
	}
	its, err := s.st.ListItems(card.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	for _, it := range its {
		if !it.Superseded && it.Status != "done" {
			writeErr(w, store.ConflictError{Msg: fmt.Sprintf("%s is not done yet.", it.Ident)})
			return
		}
	}
	rows, err := s.st.ListBranches(card.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(rows) == 0 {
		writeErr(w, store.ConflictError{Msg: "No repo is planned for this ticket. Add one under Branching first."})
		return
	}
	for _, b := range rows {
		switch {
		case b.Status == "blocked":
			writeErr(w, store.ConflictError{Msg: filepath.Base(b.Repo) + " is blocked. Resolve it in Discuss first."})
			return
		case b.Status == "planned":
			writeErr(w, store.ConflictError{Msg: filepath.Base(b.Repo) + " has no branch yet (still Planned)."})
			return
		case b.ViaDev && !store.IntegratedStatus(b.Status):
			writeErr(w, store.ConflictError{Msg: filepath.Base(b.Repo) + " goes via -dev and is not merged into it yet. Open Discuss to integrate it first."})
			return
		}
	}
	board, err := s.st.GetBoard(card.BoardID)
	if err != nil {
		writeErr(w, err)
		return
	}
	ready, err := s.st.PushReady(card.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []shipResult{}
	for _, b := range rows {
		if b.Status == "pr_open" {
			out = append(out, shipResult{Repo: b.Repo, Status: "skipped", PRURL: b.PRURL, Note: "PR already open"})
			continue
		}
		picked, err := s.st.PickedSHAs(card.ID, b.Repo)
		if err != nil {
			writeErr(w, err)
			return
		}
		pol := gitx.Policy{Feature: b.FeatureBranch, Dev: b.DevBranch, Base: b.BaseBranch, PRBase: board.DevBranch, Ident: card.Ident,
			Mode: b.Mode, Picked: picked, Integrated: store.IntegratedStatus(b.Status), PushReady: ready, Direct: !b.ViaDev}
		out = append(out, s.shipRepo(card, b, pol))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) shipRepo(card store.Card, b store.Branch, pol gitx.Policy) shipResult {
	res := shipResult{Repo: b.Repo}
	fail := func(err error) shipResult {
		res.Status, res.Note = "failed", err.Error()
		return res
	}
	branches := []string{b.FeatureBranch}
	head, base := b.FeatureBranch, b.BaseBranch
	if b.ViaDev {
		branches = append(branches, b.DevBranch)
		head, base = b.DevBranch, pol.PRBase
	}
	for _, br := range branches {
		if _, err := gitx.GitX(b.Repo, []string{"push", "-u", "origin", br}, pol); err != nil {
			return fail(fmt.Errorf("push %s: %w", br, err))
		}
	}
	set := func(status, pr string) {
		p := store.BranchPatch{Status: &status}
		if pr != "" {
			p.PRURL = &pr
		}
		if _, err := s.st.UpdateBranch(card.ID, b.Repo, p); err != nil {
			res.Note += " (recording it failed: " + err.Error() + ")"
		}
	}
	if k := gitx.RemoteKind(b.Repo); k != "github" {
		set("pushed", "")
		res.Status, res.Note = "pushed", "Pushed "+strings.Join(branches, ", ")+". The remote is not GitHub, so open the PR there."
		return res
	}
	body := card.Description
	if r := []rune(body); len(r) > 3000 {
		body = string(r[:3000]) + "…"
	}
	title := card.Ident + ": " + card.Title
	if r := []rune(title); len(r) > 200 {
		title = string(r[:200])
	}
	url, err := gitx.GH(b.Repo, []string{"pr", "create", "--base", base, "--head", head, "--title", title, "--body", body}, pol)
	if err != nil {
		set("pushed", "")
		res.Status, res.Note = "failed", "Pushed "+strings.Join(branches, ", ")+", but the PR failed: "+err.Error()
		return res
	}
	if i := strings.LastIndex(url, "\n"); i >= 0 {
		url = url[i+1:]
	}
	set("pr_open", strings.TrimSpace(url))
	res.Status, res.PRURL, res.Note = "pr_open", strings.TrimSpace(url), "Pushed and PR opened"
	return res
}
