package qa

import (
	"encoding/json"
	"sort"
	"strings"
)

// SecretsFromState collects cookie and storage values of 8+ characters from a Playwright storageState.
func SecretsFromState(b []byte) []string {
	var st struct {
		Cookies []struct{ Value string } `json:"cookies"`
		Origins []struct {
			LocalStorage []struct{ Value string } `json:"localStorage"`
		} `json:"origins"`
	}
	if json.Unmarshal(b, &st) != nil {
		return nil
	}
	set := map[string]bool{}
	for _, c := range st.Cookies {
		if len(c.Value) >= 8 {
			set[c.Value] = true
		}
	}
	for _, o := range st.Origins {
		for _, l := range o.LocalStorage {
			if len(l.Value) >= 8 {
				set[l.Value] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// Scrub replaces every secret in s.
func Scrub(s string, secrets []string) string {
	for _, x := range secrets {
		s = strings.ReplaceAll(s, x, "[redacted]")
	}
	return s
}

// StateExpired reports a saved login that cannot be valid any more: unreadable, no cookies, or every cookie
// that carries an expiry is already past (session cookies carry none and cannot be judged here).
func StateExpired(b []byte, now int64) bool {
	var st struct {
		Cookies []struct {
			Expires float64 `json:"expires"`
		} `json:"cookies"`
	}
	if json.Unmarshal(b, &st) != nil || len(st.Cookies) == 0 {
		return true
	}
	withExpiry := 0
	for _, c := range st.Cookies {
		if c.Expires <= 0 {
			return false // a session cookie: judged by the live check
		}
		if int64(c.Expires) > now {
			return false
		}
		withExpiry++
	}
	return withExpiry > 0
}
