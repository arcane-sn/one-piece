package api

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// 1x1 PNG
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestUploadImageRoundTrip(t *testing.T) {
	s := newTestServer(t)
	ck := s.login(t)
	resp, body := s.req(t, "POST", "/api/uploads", map[string]string{"data": "data:image/png;base64," + tinyPNG}, ck)
	var out struct{ URL string }
	json.Unmarshal(body, &out)
	if resp.StatusCode != 201 || !strings.HasPrefix(out.URL, "/api/uploads/") || !strings.HasSuffix(out.URL, ".png") {
		t.Fatalf("upload: %d %s", resp.StatusCode, body)
	}
	resp, body = s.req(t, "GET", out.URL, nil, ck)
	want, _ := base64.StdEncoding.DecodeString(tinyPNG)
	if resp.StatusCode != 200 || string(body) != string(want) || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download: %d, %d bytes", resp.StatusCode, len(body))
	}
	if _, body2 := s.req(t, "POST", "/api/uploads", map[string]string{"data": tinyPNG}, ck); !strings.Contains(string(body2), out.URL) {
		t.Errorf("same image must map to the same url: %s", body2)
	}
	if resp, _ = s.req(t, "GET", out.URL, nil, nil); resp.StatusCode != 401 {
		t.Errorf("anonymous download: %d, want 401", resp.StatusCode)
	}
}

func TestUploadRejectsNonImagesAndBadNames(t *testing.T) {
	s := newTestServer(t)
	ck := s.login(t)
	svg := base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	for _, data := range []string{svg, base64.StdEncoding.EncodeToString([]byte("hello")), "%%%", ""} {
		if resp, _ := s.req(t, "POST", "/api/uploads", map[string]string{"data": data}, ck); resp.StatusCode != 400 {
			t.Errorf("data %.20q: %d, want 400", data, resp.StatusCode)
		}
	}
	for _, name := range []string{"x.png", "..%2Fone-piece.db", strings.Repeat("a", 64) + ".svg", strings.Repeat("a", 64) + ".png"} {
		if resp, _ := s.req(t, "GET", "/api/uploads/"+name, nil, ck); resp.StatusCode != 404 {
			t.Errorf("name %q: %d, want 404", name, resp.StatusCode)
		}
	}
}
