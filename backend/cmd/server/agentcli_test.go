package main

import (
	"bytes"
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
