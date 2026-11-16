package qa

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestHelperServer is the "dev server" the stack tests start (re-exec of the test binary).
func TestHelperServer(t *testing.T) {
	if os.Getenv("QA_HELPER") != "1" {
		t.Skip()
	}
	http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
}

func freePort(t *testing.T) int {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func helperSvc(t *testing.T, name string, port int, cmd string) Service {
	if cmd == "" {
		cmd = "QA_HELPER=1 '" + os.Args[0] + "' -test.run=TestHelperServer"
	}
	return Service{Name: name, Folder: t.TempDir(), Command: cmd, Port: port, ReadyURL: "http://127.0.0.1:" + strconv.Itoa(port) + "/", ReadyTimeoutS: 10}
}

func TestStackStartReadyStop(t *testing.T) {
	p := freePort(t)
	dir := t.TempDir()
	st, err := StartStack(t.Context(), []Service{helperSvc(t, "web", p, "")}, filepath.Join(dir, "logs"), filepath.Join(dir, "stack.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !PortBusy(p) {
		t.Error("service should hold its port")
	}
	if _, err := os.Stat(filepath.Join(dir, "stack.json")); err != nil {
		t.Error("state file expected")
	}
	st.Stop()
	if PortBusy(p) {
		t.Error("Stop must free the port")
	}
	if _, err := os.Stat(filepath.Join(dir, "stack.json")); err == nil {
		t.Error("state file should be removed")
	}
}

func TestStackRefusesBusyPortAndNeverReuses(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	dir := t.TempDir()
	_, err := StartStack(t.Context(), []Service{helperSvc(t, "web", port, "")}, filepath.Join(dir, "l"), filepath.Join(dir, "s.json"))
	if err == nil || !strings.Contains(err.Error(), "did not start") {
		t.Fatalf("busy port: %v", err)
	}
}

func TestStackNotReadyAndEarlyExit(t *testing.T) {
	dir := t.TempDir()
	p := freePort(t)
	s := helperSvc(t, "web", p, "sleep 30")
	s.ReadyTimeoutS = 1
	start := time.Now()
	if _, err := StartStack(t.Context(), []Service{s}, filepath.Join(dir, "l"), filepath.Join(dir, "s.json")); err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("timeout: %v", err)
	}
	if time.Since(start) > 15*time.Second {
		t.Error("took too long")
	}
	if PortBusy(p) {
		t.Error("failed start must clean up")
	}
	s = helperSvc(t, "web", freePort(t), "exit 3")
	if _, err := StartStack(t.Context(), []Service{s}, filepath.Join(dir, "l"), filepath.Join(dir, "s.json")); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("early exit: %v", err)
	}
}

func TestCleanupStaleChecksCommand(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "stack.json")
	victim := exec.Command("sleep", "60")
	victim.SysProcAttr = sysProcGroup()
	victim.Start()
	defer victim.Process.Kill()
	// a stored command that does not match the PID's real command: must not be killed
	os.WriteFile(f, []byte(fmt.Sprintf(`[{"pid":%d,"command":"npm start"}]`, victim.Process.Pid)), 0o600)
	CleanupStale(f)
	if victim.ProcessState != nil {
		t.Fatal("wrong command must not be killed")
	}
	if _, err := os.Stat(f); err == nil {
		t.Error("state file should be removed either way")
	}
	os.WriteFile(f, []byte(fmt.Sprintf(`[{"pid":%d,"command":"sleep 60"}]`, victim.Process.Pid)), 0o600)
	CleanupStale(f)
	done := make(chan error, 1)
	go func() { done <- victim.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("matching command should be killed")
	}
}

func TestSnapshotAndBackstop(t *testing.T) {
	repo := t.TempDir()
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@x", "commit", "-q", "--allow-empty", "-m", "i"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	a, err := Snapshot(repo)
	if err != nil || a.Branch != "main" || a.SHA == "" || a.Dirty {
		t.Fatalf("snapshot = %+v %v", a, err)
	}
	same, _ := Snapshot(repo)
	if c := Changed([]RepoState{a}, []RepoState{same}); len(c) != 0 {
		t.Errorf("unchanged repo reported: %v", c)
	}
	os.WriteFile(filepath.Join(repo, "x.txt"), []byte("x"), 0o644)
	b, _ := Snapshot(repo)
	if !b.Dirty || len(Changed([]RepoState{a}, []RepoState{b})) != 1 {
		t.Errorf("a new file must trip the backstop: %+v", b)
	}
}
