package main

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// agentCommands are the subcommands Claude may run during a brainstorming session.
// The server gives it the database path and board id through the environment, so Claude
// cannot point them elsewhere; every write goes through store's Draft guards.
var agentCommands = map[string]bool{"tickets": true, "ticket": true, "draft-add": true, "draft-update": true}

// runAgentCLI runs one agent subcommand (args[0] is its name).
//
//	tickets                      one line per ticket: #id status title
//	ticket <id>                  one ticket in full
//	draft-add <title>            new Draft, description on stdin
//	draft-update <id> <title>    change a Draft, description on stdin
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

	switch args[0] {
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
