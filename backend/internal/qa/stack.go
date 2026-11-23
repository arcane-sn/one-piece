package qa

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// PortBusy reports whether anything holds or answers on a loopback port (IPv4 or IPv6).
func PortBusy(port int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
			c.Close()
			return true
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			if strings.Contains(err.Error(), "address already in use") {
				return true
			}
			continue // no IPv6 on this machine, for example
		}
		ln.Close()
	}
	return false
}

type proc struct {
	svc  Service
	cmd  *exec.Cmd
	done chan struct{}
}

// Stack is the running QA stack of one run.
type Stack struct {
	procs     []*proc
	stateFile string
}

type stateEntry struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
}

func writeState(file string, ps []*proc) {
	var es []stateEntry
	for _, p := range ps {
		es = append(es, stateEntry{p.cmd.Process.Pid, p.svc.Command})
	}
	b, _ := json.Marshal(es)
	os.WriteFile(file, b, 0o600)
}

// CleanupStale kills process groups a previous server left behind (after checking the PID still runs
// the command we stored, so a reused PID is never killed) and removes the state file.
func CleanupStale(stateFile string) {
	b, err := os.ReadFile(stateFile)
	if err != nil {
		return
	}
	var es []stateEntry
	json.Unmarshal(b, &es)
	for _, e := range es {
		out, err := exec.Command("ps", "-p", strconv.Itoa(e.PID), "-o", "command=").Output()
		if err == nil && strings.Contains(string(out), strings.Fields(e.Command + " ")[0]) {
			syscall.Kill(-e.PID, syscall.SIGKILL)
		}
	}
	os.Remove(stateFile)
}

// StartStack starts every service in its own process group, then waits until each ready URL answers.
// Ports must be free first: a server One Piece did not start is never reused. Output goes to logDir/<name>.log.
func StartStack(ctx context.Context, svcs []Service, logDir, stateFile string) (*Stack, error) {
	var ext []Service // already running: checked, never started or stopped
	own := svcs[:0:0]
	for _, s := range svcs {
		switch {
		case s.External:
			if !PortBusy(s.Port) {
				return nil, fmt.Errorf("%s is not running on port %d. Start it first (it is marked as already running).", s.Name, s.Port)
			}
			ext = append(ext, s)
		case PortBusy(s.Port):
			return nil, fmt.Errorf("Port %d is already in use by a process One Piece did not start. Stop it and run again.", s.Port)
		default:
			own = append(own, s)
		}
	}
	svcs = own
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, err
	}
	st := &Stack{stateFile: stateFile}
	for _, s := range svcs {
		lf, err := os.OpenFile(filepath.Join(logDir, s.Name+".log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			st.Stop()
			return nil, err
		}
		cmd := exec.Command("sh", "-c", s.Command)
		cmd.Dir = s.Folder
		cmd.Env = append(os.Environ(), "PORT="+strconv.Itoa(s.Port), "BROWSER=none")
		cmd.Stdout, cmd.Stderr = lf, lf
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			lf.Close()
			st.Stop()
			return nil, fmt.Errorf("could not start %s: %v", s.Name, err)
		}
		p := &proc{svc: s, cmd: cmd, done: make(chan struct{})}
		go func() { cmd.Wait(); lf.Close(); close(p.done) }()
		st.procs = append(st.procs, p)
		writeState(stateFile, st.procs)
	}
	for _, p := range st.procs {
		if err := waitReady(ctx, p); err != nil {
			st.Stop()
			return nil, err
		}
	}
	for _, s := range ext { // no process of ours behind it (p.done stays nil)
		if err := waitReady(ctx, &proc{svc: s}); err != nil {
			st.Stop()
			return nil, err
		}
	}
	return st, nil
}

func waitReady(ctx context.Context, p *proc) error {
	timeout := time.Duration(p.svc.ReadyTimeoutS) * time.Second
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.done:
			return fmt.Errorf("%s exited before it was ready; see its log", p.svc.Name)
		default:
		}
		if resp, err := client.Get(p.svc.ReadyURL); err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s was not ready after %d s; see its log", p.svc.Name, p.svc.ReadyTimeoutS)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// Stop ends every group (SIGTERM, then SIGKILL after 5 s) and waits for the ports to be free.
func (s *Stack) Stop() {
	for _, p := range s.procs {
		syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	}
	for _, p := range s.procs {
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			<-p.done
		}
		syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL) // stragglers of the group
	}
	for _, p := range s.procs {
		for i := 0; i < 50 && PortBusy(p.svc.Port); i++ {
			time.Sleep(100 * time.Millisecond)
		}
	}
	os.Remove(s.stateFile)
}
