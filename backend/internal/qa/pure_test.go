package qa

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostAllowed(t *testing.T) {
	allowed := []string{"example.test", "*.cdn.example.test"}
	for h, want := range map[string]bool{
		"example.test": true, "EXAMPLE.test.": true, "example.test:8443": true, "localhost:5000": true, "127.0.0.1": true, "[::1]:80": true,
		"evil-example.test": false, "example.test.evil.test": false, "a.cdn.example.test": true, "cdn.example.test": false,
		"x.example.test": false, "93.184.216.34": false, "": false, "example.test@evil.test": false,
	} {
		if got := HostAllowed(h, allowed); got != want {
			t.Errorf("HostAllowed(%q) = %v, want %v", h, got, want)
		}
	}
	if _, ok := NormalizeHost("http://x"); ok {
		t.Error("a URL is not a host")
	}
	if h, ok := NormalizeHost(" *.Example.test. "); !ok || h != "*.example.test" {
		t.Errorf("normalize = %q %v", h, ok)
	}
}

func TestProxy(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "hello") }))
	defer up.Close()
	p, err := NewProxy([]string{"example.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	pu, _ := url.Parse("http://" + p.Addr())
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
	// loopback passes (plain HTTP through the proxy)
	resp, err := c.Get(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("loopback via proxy: %d", resp.StatusCode)
	}
	// a host outside the list is refused and recorded
	resp, err = c.Get("http://blocked.example.org/x")
	if err != nil || resp.StatusCode != 403 {
		t.Fatalf("blocked: %v %v", resp, err)
	}
	resp.Body.Close()
	// CONNECT to a non-allowed host is refused; to loopback it tunnels
	conn, _ := net.Dial("tcp", p.Addr())
	fmt.Fprintf(conn, "CONNECT tracker.example.org:443 HTTP/1.1\r\nHost: tracker.example.org:443\r\n\r\n")
	line, _ := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if !strings.Contains(line, "403") {
		t.Errorf("CONNECT status = %q", line)
	}
	echo, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() {
		c, _ := echo.Accept()
		c.Write([]byte("pong"))
		c.Close()
	}()
	conn, _ = net.Dial("tcp", p.Addr())
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", echo.Addr(), echo.Addr())
	r := bufio.NewReader(conn)
	if l, _ := r.ReadString('\n'); !strings.Contains(l, "200") {
		t.Errorf("CONNECT loopback = %q", l)
	}
	conn.Close()
	b := p.Blocked()
	if len(b) != 2 || b[0] != "blocked.example.org" || b[1] != "tracker.example.org" {
		t.Errorf("blocked = %v", b)
	}
}

const goodSpec = `import { test, expect } from './helpers'
// uses process in a comment only
test('cart', async ({ page }) => {
  await qaStep(page, 'Open', async () => { await page.goto('/'); await expect(page).toHaveTitle(/x/) })
})`

func TestLintSpec(t *testing.T) {
	if bad := LintSpec(goodSpec); len(bad) != 0 {
		t.Fatalf("good spec rejected: %v", bad)
	}
	for name, src := range map[string]string{
		"import fs":      "import fs from 'fs'\n" + goodSpec,
		"playwright":     "import { test } from '@playwright/test'\n" + goodSpec,
		"require":        goodSpec + "\nrequire('fs')",
		"dynamic import": goodSpec + "\nawait import('fs')",
		"process":        goodSpec + "\nprocess.env.X",
		"eval":           goodSpec + "\neval('1')",
		"fetch":          goodSpec + "\nfetch('http://x')",
		"launch":         goodSpec + "\nchromium.launch()",
		"new context":    goodSpec + "\nbrowser.newContext()",
		"no step":        "import { test, expect } from './helpers'\ntest('x', async () => { expect(1) })",
		"no expect":      "import { test } from './helpers'\ntest('x', async ({page}) => { await qaStep(page,'a',async()=>{}) })",
		"too long":       goodSpec + strings.Repeat("\n// x", 400),
		"after url":      goodSpec + "\nconst u = 'http://x'; eval('1')",
	} {
		if bad := LintSpec(src); len(bad) == 0 {
			t.Errorf("%s: should be rejected", name)
		}
	}
}

func TestScrub(t *testing.T) {
	st := []byte(`{"cookies":[{"name":"s","value":"abcdef123456"},{"name":"x","value":"short"}],"origins":[{"origin":"o","localStorage":[{"name":"t","value":"tok-987654321"}]}]}`)
	sec := SecretsFromState(st)
	if len(sec) != 2 {
		t.Fatalf("secrets = %v", sec)
	}
	got := Scrub("fail: abcdef123456 and tok-987654321 short", sec)
	if strings.Contains(got, "abcdef") || strings.Contains(got, "tok-98") || !strings.Contains(got, "short") {
		t.Errorf("scrubbed = %q", got)
	}
	if SecretsFromState([]byte("nope")) != nil {
		t.Error("garbage state")
	}
}

func rep(stats string, errs string, suites string) []byte {
	return []byte(`{"stats":` + stats + `,"errors":` + errs + `,"suites":` + suites + `}`)
}

func TestClassify(t *testing.T) {
	ok := []byte(`[{"position":1,"title":"a","status":"passed","screenshot":"steps/01-a.png"}]`)
	failStep := []byte(`[{"position":1,"title":"a","status":"failed","screenshot":"steps/01-a.png","error":"x"}]`)
	loginStep := []byte(`[{"position":1,"title":"a","status":"failed","loginRedirect":true}]`)
	suite := func(msg string) string {
		return `[{"suites":[{"specs":[{"tests":[{"results":[{"status":"failed","error":{"message":"` + msg + `"}}]}]}]}]}]`
	}
	for name, c := range map[string]struct {
		a       Attempt
		blocked []string
		kind    string
	}{
		"passed":     {ParseAttempt(rep(`{"expected":1}`, `[]`, `[]`), ok), nil, "passed"},
		"assertion":  {ParseAttempt(rep(`{"unexpected":1}`, `[]`, suite("expect(locator).toBeVisible() failed")), failStep), nil, "assertion"},
		"compile":    {ParseAttempt(rep(`{}`, `[{"message":"SyntaxError: x"}]`, `[]`), nil), nil, "spec"},
		"no tests":   {ParseAttempt(rep(`{}`, `[]`, `[]`), nil), nil, "spec"},
		"garbage":    {ParseAttempt([]byte("<html>"), nil), nil, "spec"},
		"refused":    {ParseAttempt(rep(`{"unexpected":1}`, `[]`, suite("page.goto: net::ERR_CONNECTION_REFUSED at x")), failStep), nil, "infra"},
		"login":      {ParseAttempt(rep(`{"unexpected":1}`, `[]`, suite("x")), loginStep), nil, "infra"},
		"blocked":    {ParseAttempt(rep(`{"unexpected":1}`, `[]`, suite("timeout")), failStep), []string{"cdn.example.org"}, "infra"},
		"pass+steps": {ParseAttempt(rep(`{"expected":1}`, `[]`, `[]`), failStep), nil, "assertion"},
	} {
		if got := Classify(c.a, c.blocked); got.Kind != c.kind {
			t.Errorf("%s: %+v, want %s", name, got, c.kind)
		}
	}
	if a := ParseAttempt(rep(`{"unexpected":1}`, `[]`, suite("expect failed")), failStep); !strings.Contains(a.FirstError, "expect failed") {
		t.Errorf("first error = %q", a.FirstError)
	}
}

func TestEvidenceNames(t *testing.T) {
	for n, want := range map[string]bool{
		"steps/01-open-cart.png": true, "trace.zip": true, "video.webm": true, "result.json": true, "repos.json": true,
		"../x.png": false, "steps/../a.png": false, "auth/x.json": false, "steps/01-a.svg": false, "services/a.log": false,
		"steps/1-a.png": false, "steps/01-A.png": false, "%2e%2e/x": false, "": false,
	} {
		if ValidEvidenceFile(n) != want {
			t.Errorf("%q = %v", n, !want)
		}
	}
}

func TestConfigValidate(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	svc := filepath.Join(root, "web")
	os.MkdirAll(svc, 0o755)
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	os.Symlink(outside, filepath.Join(root, "link"))
	mk := func() Config {
		return Config{BaseURL: "http://localhost:5000", AllowedHosts: []string{"auth.example.test"},
			Services: []Service{{Name: "web", Folder: svc, Command: "npm start", Port: 5000, ReadyURL: "http://localhost:5000/"}}}
	}
	c := mk()
	if err := c.Validate([]string{root}); err != nil || c.LoginURL != c.BaseURL || c.Services[0].ReadyTimeoutS != 120 || !c.Configured() {
		t.Fatalf("good config: %v %+v", err, c)
	}
	for name, edit := range map[string]func(*Config){
		"folder outside":   func(c *Config) { c.Services[0].Folder = outside },
		"symlink escape":   func(c *Config) { c.Services[0].Folder = filepath.Join(root, "link") },
		"low port":         func(c *Config) { c.Services[0].Port = 80; c.Services[0].ReadyURL = "http://localhost:80/" },
		"dup port":         func(c *Config) { c.Services = append(c.Services, c.Services[0]); c.Services[1].Name = "b" },
		"remote ready url": func(c *Config) { c.Services[0].ReadyURL = "http://example.test:5000/" },
		"other port ready": func(c *Config) { c.Services[0].ReadyURL = "http://localhost:5001/" },
		"base host":        func(c *Config) { c.BaseURL = "https://shop.example.test" },
		"bad host":         func(c *Config) { c.AllowedHosts = []string{"https://x"} },
		"empty name":       func(c *Config) { c.Services[0].Name = " !! " },
		"no command":       func(c *Config) { c.Services[0].Command = " " },
		"no services":      func(c *Config) { c.Services = nil },
		"not http":         func(c *Config) { c.BaseURL = "ftp://localhost" },
	} {
		c := mk()
		edit(&c)
		if err := c.Validate([]string{root}); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
}

func TestNoLoginCase(t *testing.T) {
	for _, b := range []string{"## Prerequisites\n- Login: no login\n- Data: x", "- **Login:** no login", "* login: None", "Login: tidak perlu login"} {
		if !NoLoginCase(b) {
			t.Errorf("%q should be no-login", b)
		}
	}
	for _, b := range []string{"- Login: admin", "- Login: a user without a PIN", "Steps: no login page is shown", ""} {
		if NoLoginCase(b) {
			t.Errorf("%q needs login", b)
		}
	}
	p := Paths{Data: "/d"}
	if strings.Contains(p.PlaywrightConfig(1, "X-1", "/a", "p:1", Config{BaseURL: "http://x", NoLogin: true}), "storageState") {
		t.Error("no-login config must not load a saved login")
	}
	if !strings.Contains(p.PlaywrightConfig(1, "X-1", "/a", "p:1", Config{BaseURL: "http://x"}), "storageState") {
		t.Error("normal config loads the saved login")
	}
}

func TestLintAllowsBannedWordInUrlPath(t *testing.T) {
	ok := "import { test, expect, qaStep } from './helpers'\ntest('x', async ({ page }) => { await qaStep(page, 's', async () => { await page.goto('/v1/cimb/binding/process/ok-123'); expect(1).toBe(1) }) })"
	if bad := LintSpec(ok); len(bad) > 0 {
		t.Fatalf("url path rejected: %v", bad)
	}
	if bad := LintSpec(strings.Replace(ok, "expect(1)", "process.env; expect(1)", 1)); len(bad) == 0 {
		t.Fatal("process.env must still be rejected")
	}
}
