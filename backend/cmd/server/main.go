// Command server runs One Piece, or creates its single user.
//
//	server [-addr 127.0.0.1:5300] [-db one-piece.db] [-static ../frontend/dist]
//	server create-user [-db one-piece.db] <username>
//	server reset-password [-db one-piece.db] <username>
//	server qa-setup [-db one-piece.db]   install the Playwright toolchain for browser QA (needs npm and network, once)
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/arcane-sn/one-piece/backend/internal/api"
	"github.com/arcane-sn/one-piece/backend/internal/qa"
	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// ListenAddrOK reports whether addr binds only to the loopback interface.
func ListenAddrOK(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "create-user" {
		createUser(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		resetPassword(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "qa-setup" {
		qaSetup(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && agentCommands[os.Args[1]] {
		if err := runAgentCLI(os.Args[1:], os.Getenv, os.Stdin, os.Stdout); err != nil {
			fatal("%v", err)
		}
		return
	}
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:5300", "listen address (loopback only)")
	db := fs.String("db", "one-piece.db", "SQLite file")
	static := fs.String("static", "../frontend/dist", "built frontend directory")
	fs.Parse(os.Args[1:])

	if !ListenAddrOK(*addr) {
		fatal("refusing to listen on %q: One Piece only binds to loopback (127.0.0.1, localhost, ::1)", *addr)
	}
	st, err := store.Open(*db)
	if err != nil {
		fatal("open db: %v", err)
	}
	srv := &http.Server{Addr: *addr, Handler: api.New(st, *static), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("One Piece on http://%s", *addr)
	fatal("%v", srv.ListenAndServe())
}

func createUser(args []string) {
	fs := flag.NewFlagSet("create-user", flag.ExitOnError)
	db := fs.String("db", "one-piece.db", "SQLite file")
	fs.Parse(args)
	if fs.NArg() != 1 {
		fatal("usage: server create-user [-db file] <username>")
	}
	pw := readPassword()
	st, err := store.Open(*db)
	if err != nil {
		fatal("open db: %v", err)
	}
	if err := st.CreateUser(fs.Arg(0), pw); err != nil {
		fatal("%v", err)
	}
	fmt.Println("user created")
}

func resetPassword(args []string) {
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	db := fs.String("db", "one-piece.db", "SQLite file")
	fs.Parse(args)
	if fs.NArg() != 1 {
		fatal("usage: server reset-password [-db file] <username>")
	}
	pw := readPassword()
	st, err := store.Open(*db)
	if err != nil {
		fatal("open db: %v", err)
	}
	if err := st.ResetPassword(fs.Arg(0), pw); err != nil {
		fatal("%v", err)
	}
	fmt.Println("password reset; all sessions ended")
}

// readPassword prompts without echo on a terminal, else reads one line of stdin.
func readPassword() string {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "Password: ")
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			fatal("read password: %v", err)
		}
		return string(b)
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

// qaSetup installs the browser QA toolchain next to the database (backend/qa/runner by default).
func qaSetup(args []string) {
	fs := flag.NewFlagSet("qa-setup", flag.ExitOnError)
	db := fs.String("db", "one-piece.db", "SQLite file")
	fs.Parse(args)
	abs, err := filepath.Abs(*db)
	if err != nil {
		fatal("%v", err)
	}
	p := qa.Paths{Data: filepath.Dir(abs)}
	if err := qa.Setup(p, func(s string) { fmt.Println(s) }); err != nil {
		fatal("qa-setup failed: %v", err)
	}
	fmt.Println("Playwright is ready in", p.Runner())
}
