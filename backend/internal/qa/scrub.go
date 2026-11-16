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
