package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// cliFixture opens a temp database with two boards; board 1 holds a business card and a draft.
func cliFixture(t *testing.T) (st *store.Store, env func(string) string, own, foreign int64, bizID, draftID int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.DB.Close() })
	a, _ := st.CreateBoard("a", "", nil)
	b, _ := st.CreateBoard("b", "", nil)
	biz, _ := st.CreateCard(a.ID, "Biz ticket", "business body", "")
	d, _ := st.CreateDraft(a.ID, "Draft ticket", "draft body")
	env = func(k string) string {
		return map[string]string{"ONE_PIECE_DB": path, "ONE_PIECE_BOARD": strconv.FormatInt(a.ID, 10)}[k]
	}
	return st, env, a.ID, b.ID, biz.ID, d.ID
}

func runCLI(env func(string) string, stdin string, args ...string) (string, error) {
	var out bytes.Buffer
	err := runAgentCLI(args, env, strings.NewReader(stdin), &out)
	return out.String(), err
}

func TestAgentCLITicketReadsOwnBoardOnly(t *testing.T) {
	st, env, _, foreign, biz, _ := cliFixture(t)
	out, err := runCLI(env, "", "ticket", strconv.FormatInt(biz, 10))
	if err != nil || !strings.Contains(out, "Biz ticket") || !strings.Contains(out, "business body") {
		t.Fatalf("own ticket: %q, %v", out, err)
	}
	other, _ := st.CreateCard(foreign, "Secret", "", "")
	if out, err := runCLI(env, "", "ticket", strconv.FormatInt(other.ID, 10)); err == nil || strings.Contains(out, "Secret") {
		t.Fatalf("a card of another board must not be readable: %q, %v", out, err)
	}
	if out, _ := runCLI(env, "", "tickets"); !strings.Contains(out, "draft Draft ticket") || strings.Contains(out, "Secret") {
		t.Fatalf("tickets index = %q", out)
	}
}

func TestAgentCLIDraftAddReadsDescriptionFromStdin(t *testing.T) {
	st, env, own, _, _, draft := cliFixture(t)
	out, err := runCLI(env, "line one\nline two", "draft-add", "Owner login")
	if err != nil || !strings.Contains(out, "created") {
		t.Fatalf("draft-add: %q, %v", out, err)
	}
	cs, _ := st.ListCards(own)
	var found store.Card
	for _, c := range cs {
		if c.Title == "Owner login" {
			found = c
		}
	}
	if found.Status != "draft" || found.Description != "line one\nline two" {
		t.Fatalf("created card = %+v", found)
	}
	if _, err := runCLI(env, "better", "draft-update", strconv.FormatInt(draft, 10), "Renamed"); err != nil {
		t.Fatalf("draft-update: %v", err)
	}
	if c, _ := st.GetCard(draft); c.Title != "Renamed" || c.Description != "better" {
		t.Fatalf("updated draft = %+v", c)
	}
}

func TestAgentCLIDraftUpdateRefusesBusinessCard(t *testing.T) {
	st, env, _, _, biz, _ := cliFixture(t)
	if _, err := runCLI(env, "x", "draft-update", strconv.FormatInt(biz, 10), "Hacked"); err == nil {
		t.Fatal("draft-update on a business card must fail")
	}
	if c, _ := st.GetCard(biz); c.Title != "Biz ticket" {
		t.Fatalf("business card changed: %+v", c)
	}
}

func TestAgentCLIRefusesWithoutBoardEnv(t *testing.T) {
	_, env, _, _, _, _ := cliFixture(t)
	noBoard := func(k string) string {
		if k == "ONE_PIECE_BOARD" {
			return ""
		}
		return env(k)
	}
	if _, err := runCLI(noBoard, "", "tickets"); err == nil {
		t.Fatal("must refuse without ONE_PIECE_BOARD")
	}
}

// branchFixture: a board whose folder holds a git repo (with one commit on main) and a business card.
func branchFixture(t *testing.T) (st *store.Store, env func(string) string, repo string, card store.Card) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.DB.Close() })
	master, _ := filepath.EvalSymlinks(t.TempDir())
	repo = filepath.Join(master, "svc")
	os.MkdirAll(repo, 0o755)
	os.MkdirAll(filepath.Join(master, "other"), 0o755)
	for _, d := range []string{repo, filepath.Join(master, "other")} {
		for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@x", "commit", "-q", "--allow-empty", "-m", "init"}} {
			if out, err := exec.Command("git", append([]string{"-C", d}, a...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", a, err, out)
			}
		}
	}
	b, _ := st.CreateBoard("Midas", "", []string{master})
	card, _ = st.CreateCard(b.ID, "Login", "", "")
	env = func(k string) string {
		return map[string]string{"ONE_PIECE_DB": path, "ONE_PIECE_BOARD": strconv.FormatInt(b.ID, 10), "ONE_PIECE_CARD": strconv.FormatInt(card.ID, 10)}[k]
	}
	return st, env, repo, card
}

func TestBranchPlanThenGit(t *testing.T) {
	_, env, repo, card := branchFixture(t)
	if out, err := runCLI(env, "", "branch-plan", repo, "main"); err != nil || !strings.Contains(out, "feature/"+card.Ident) {
		t.Fatalf("branch-plan: %q, %v", out, err)
	}
	if out, err := runCLI(env, "", "git", repo, "switch", "-c", "feature/"+card.Ident, "main"); err != nil {
		t.Fatalf("switch -c: %q, %v", out, err)
	}
	if out, _ := runCLI(env, "", "git", repo, "branch", "--show-current"); strings.TrimSpace(out) != "feature/"+card.Ident {
		t.Fatalf("current branch = %q", out)
	}
	if _, err := runCLI(env, "", "branch-status", repo, "branched", "cut", "from", "main"); err != nil {
		t.Fatal(err)
	}
	if out, _ := runCLI(env, "", "branches"); !strings.Contains(out, "branched") || !strings.Contains(out, repo) {
		t.Fatalf("branches = %q", out)
	}
	if _, err := runCLI(env, "", "branch-status", repo, "weird"); err == nil {
		t.Error("unknown status must fail")
	}
	if _, err := runCLI(env, "", "branch-pr", repo, "https://example.com/pr/9"); err != nil {
		t.Fatal(err)
	}
	if out, _ := runCLI(env, "", "branches"); !strings.Contains(out, "pr_open") || !strings.Contains(out, "https://example.com/pr/9") {
		t.Fatalf("after branch-pr = %q", out)
	}
}

func TestGitRequiresRegisteredRepo(t *testing.T) {
	_, env, repo, _ := branchFixture(t)
	runCLI(env, "", "branch-plan", repo, "main")
	other := filepath.Join(filepath.Dir(repo), "other")
	if _, err := runCLI(env, "", "git", other, "status"); err == nil || !strings.Contains(err.Error(), "branch-plan") {
		t.Fatalf("unregistered repo: %v", err)
	}
	if _, err := runCLI(env, "", "git", "svc", "status"); err == nil {
		t.Fatal("relative repo path must fail")
	}
	if _, err := runCLI(env, "", "gh", other, "pr", "view", "x"); err == nil || !strings.Contains(err.Error(), "branch-plan") {
		t.Fatalf("gh unregistered: %v", err)
	}
}

func TestGitSubcommandStillGuarded(t *testing.T) {
	_, env, repo, _ := branchFixture(t)
	runCLI(env, "", "branch-plan", repo, "main")
	if _, err := runCLI(env, "", "git", repo, "push", "--force", "origin", "x"); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("force push: %v", err)
	}
	if _, err := runCLI(env, "", "gh", repo, "pr", "merge", "1"); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("gh pr merge: %v", err)
	}
}

func TestAgentCLIRequiresCardEnv(t *testing.T) {
	_, env, repo, _ := branchFixture(t)
	noCard := func(k string) string {
		if k == "ONE_PIECE_CARD" {
			return ""
		}
		return env(k)
	}
	for _, args := range [][]string{{"branches"}, {"branch-plan", repo, "main"}, {"git", repo, "status"}} {
		if _, err := runCLI(noCard, "", args...); err == nil || !strings.Contains(err.Error(), "ONE_PIECE_CARD") {
			t.Errorf("%v without card: %v", args, err)
		}
	}
	if _, err := runCLI(noCard, "", "tickets"); err != nil {
		t.Errorf("tickets must work without a card: %v", err)
	}
}

func TestAgentCLIRefusesCardOfAnotherBoard(t *testing.T) {
	st, env, repo, _ := branchFixture(t)
	other, _ := st.CreateBoard("Other", "", nil)
	foreign, _ := st.CreateCard(other.ID, "Foreign", "", "")
	wrong := func(k string) string {
		if k == "ONE_PIECE_CARD" {
			return strconv.FormatInt(foreign.ID, 10)
		}
		return env(k)
	}
	if _, err := runCLI(wrong, "", "branch-plan", repo, "main"); err == nil {
		t.Fatal("a card of another board must be refused")
	}
}

// reviewEnv: a Claude Review session for one ticket.
func reviewEnv(env func(string) string, card int64) func(string) string {
	return func(k string) string {
		switch k {
		case "ONE_PIECE_CARD":
			return strconv.FormatInt(card, 10)
		case "ONE_PIECE_MODE":
			return "review"
		}
		return env(k)
	}
}

func TestReviewCommandsEditTicketFlowsAndRecordVerdict(t *testing.T) {
	st, env, _, _, biz, _ := cliFixture(t)
	renv := reviewEnv(env, biz)

	out, err := runCLI(renv, "", "ticket-show")
	if err != nil || !strings.Contains(out, "Biz ticket") || !strings.Contains(out, "business body") {
		t.Fatalf("show: %q %v", out, err)
	}
	if out, err := runCLI(renv, "## New body\n- [ ] done", "ticket-edit", "Better title"); err != nil || !strings.Contains(out, "updated") {
		t.Fatalf("edit: %q %v", out, err)
	}
	c, _ := st.GetCard(biz)
	if c.Title != "Better title" || !strings.Contains(c.Description, "New body") || c.ContentVersion != 2 {
		t.Fatalf("card = %+v", c)
	}
	msgs, _ := st.ListReviewMessages(biz)
	if len(msgs) != 1 || msgs[0].Role != "event" || msgs[0].Ref == 0 {
		t.Fatalf("an edit leaves an Undo-able event: %+v", msgs)
	}

	if _, err := runCLI(renv, "title: F\nUser -> App: opens", "flow-add", "Sign in"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(renv, "just prose", "flow-add", "Bad"); err == nil {
		t.Fatal("a flow with no step must be refused")
	}
	fl, _ := st.ListFlows(biz)
	if len(fl) != 1 {
		t.Fatalf("flows = %+v", fl)
	}
	id := strconv.FormatInt(fl[0].ID, 10)
	if _, err := runCLI(renv, "title: F\nUser -> App: opens\nApp --> User: shows", "flow-update", id, "Sign in v2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetFlow(fl[0].ID); got.Title != "Sign in v2" {
		t.Fatalf("flow = %+v", got)
	}
	if out, _ := runCLI(renv, "", "ticket-show"); !strings.Contains(out, "Sign in v2") || !strings.Contains(out, id) {
		t.Fatalf("show lists flows with ids: %q", out)
	}
	if _, err := runCLI(renv, "", "flow-delete", id); err != nil {
		t.Fatal(err)
	}

	// the verdict is for the ticket as it stands now
	if _, err := runCLI(renv, "Clear and small", "review-verdict", "needs_work", "Define the timeout"); err != nil {
		t.Fatal(err)
	}
	r, _ := st.LatestReview(biz)
	c, _ = st.GetCard(biz)
	if r.Verdict != "needs_work" || r.ContentVersion != c.ContentVersion || len(r.Missing) != 1 || r.Summary != "Clear and small" {
		t.Fatalf("review = %+v", r)
	}
	if _, err := runCLI(renv, "ok", "review-verdict", "maybe"); err == nil {
		t.Fatal("bad verdict")
	}
}

func TestReviewCommandsAreScopedToTheReviewSession(t *testing.T) {
	st, env, _, foreign, biz, _ := cliFixture(t)
	// no review mode (a Discuss or Brainstorm session): refused
	discuss := func(k string) string {
		if k == "ONE_PIECE_CARD" {
			return strconv.FormatInt(biz, 10)
		}
		return env(k)
	}
	if _, err := runCLI(discuss, "x", "ticket-edit", "Hacked"); err == nil {
		t.Fatal("ticket-edit must only work in a review session")
	}
	// a flow of another ticket cannot be touched
	other, _ := st.CreateCard(foreign, "Other", "", "")
	f, _ := st.CreateFlow(other.ID, "Theirs", "A -> B: x")
	if _, err := runCLI(reviewEnv(env, biz), "A -> B: y", "flow-update", strconv.FormatInt(f.ID, 10), "Mine now"); err == nil {
		t.Fatal("another ticket's flow must be untouchable")
	}
	if _, err := runCLI(reviewEnv(env, biz), "", "flow-delete", strconv.FormatInt(f.ID, 10)); err == nil {
		t.Fatal("another ticket's flow must be undeletable")
	}
	if got, _ := st.GetFlow(f.ID); got.Title != "Theirs" {
		t.Fatalf("flow changed: %+v", got)
	}
	// a card of another board is not reachable even in review mode
	if _, err := runCLI(reviewEnv(env, other.ID), "", "ticket-show"); err == nil {
		t.Fatal("a card of another board must not be reachable")
	}
}
