package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

type testSrv struct {
	*httptest.Server
	St *store.Store
}

// newTestServer returns a running API with user owner/password123.
func newTestServer(t *testing.T) *testSrv { return newTestServerDir(t, "") }

func newTestServerDir(t *testing.T, staticDir string) *testSrv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser("owner", "password123"); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(st, staticDir))
	t.Cleanup(func() { ts.Close(); st.DB.Close() })
	return &testSrv{ts, st}
}

// req sends a JSON request, optionally with a session cookie.
func (s *testSrv) req(t *testing.T, method, path string, body any, c *http.Cookie) (*http.Response, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, s.URL+path, rd)
	r.Header.Set("Content-Type", "application/json")
	if c != nil {
		r.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

// login returns the session cookie for owner.
func (s *testSrv) login(t *testing.T) *http.Cookie {
	t.Helper()
	resp, _ := s.req(t, "POST", "/api/login", map[string]string{"username": "owner", "password": "password123"}, nil)
	for _, c := range resp.Cookies() {
		if c.Name == "op_session" {
			return c
		}
	}
	t.Fatal("no op_session cookie")
	return nil
}

func TestLoginSetsCookie(t *testing.T) {
	s := newTestServer(t)
	resp, _ := s.req(t, "POST", "/api/login", map[string]string{"username": "owner", "password": "password123"}, nil)
	if resp.StatusCode != 204 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	c := s.login(t)
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Errorf("cookie flags: %+v", c)
	}
}

func TestLoginWrongPasswordSameAsUnknownUser(t *testing.T) {
	s := newTestServer(t)
	r1, b1 := s.req(t, "POST", "/api/login", map[string]string{"username": "owner", "password": "nope-nope"}, nil)
	r2, b2 := s.req(t, "POST", "/api/login", map[string]string{"username": "ghost", "password": "nope-nope"}, nil)
	if r1.StatusCode != 401 || r2.StatusCode != 401 {
		t.Fatalf("statuses %d %d", r1.StatusCode, r2.StatusCode)
	}
	if !bytes.Equal(b1, b2) {
		t.Errorf("bodies differ: %s vs %s", b1, b2)
	}
}

func TestBoardsRequireAuth(t *testing.T) {
	s := newTestServer(t)
	if resp, _ := s.req(t, "GET", "/api/boards", nil, nil); resp.StatusCode != 401 {
		t.Errorf("no cookie: %d", resp.StatusCode)
	}
	fake := &http.Cookie{Name: "op_session", Value: "deadbeef"}
	if resp, _ := s.req(t, "GET", "/api/boards", nil, fake); resp.StatusCode != 401 {
		t.Errorf("fake cookie: %d", resp.StatusCode)
	}
}

func TestMutationNeedsJSONContentType(t *testing.T) {
	s := newTestServer(t)
	resp, err := http.Post(s.URL+"/api/login", "text/plain", bytes.NewReader([]byte(`{"username":"owner","password":"password123"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 415 {
		t.Errorf("status %d, want 415", resp.StatusCode)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	s := newTestServer(t)
	c := s.login(t)
	if resp, _ := s.req(t, "GET", "/api/me", nil, c); resp.StatusCode != 200 {
		t.Fatalf("me before logout: %d", resp.StatusCode)
	}
	if resp, _ := s.req(t, "POST", "/api/logout", nil, c); resp.StatusCode != 204 {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if resp, _ := s.req(t, "GET", "/api/me", nil, c); resp.StatusCode != 401 {
		t.Errorf("me after logout: %d", resp.StatusCode)
	}
}
