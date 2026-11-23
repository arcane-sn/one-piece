package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// usageStore receives what every Claude run reports: token use per model, the model in use, and the
// account's rate-limit windows (the CLI emits "rate_limit_event" with 5-hour and 7-day utilization).
var usageStore *store.Store

// observeUsage reads one stream-json line from a Claude process. Lines it does not care about are ignored.
func observeUsage(line []byte) {
	st := usageStore
	if st == nil {
		return
	}
	var ev struct {
		Type          string `json:"type"`
		Subtype       string `json:"subtype"`
		Model         string `json:"model"`
		RateLimitInfo struct {
			Windows map[string]struct {
				Utilization float64 `json:"utilization"`
				ResetsAt    int64   `json:"resetsAt"`
				Status      string  `json:"status"`
			} `json:"unifiedWindows"`
		} `json:"rate_limit_info"`
		ModelUsage map[string]struct {
			In    int64   `json:"inputTokens"`
			Out   int64   `json:"outputTokens"`
			CRead int64   `json:"cacheReadInputTokens"`
			CWrit int64   `json:"cacheCreationInputTokens"`
			Cost  float64 `json:"costUSD"`
		} `json:"modelUsage"`
	}
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	now := time.Now().Unix()
	switch {
	case ev.Type == "system" && ev.Subtype == "init" && ev.Model != "":
		_ = st.SetUsageModel(ev.Model)
	case ev.Type == "rate_limit_event":
		for name, w := range ev.RateLimitInfo.Windows {
			if name == "five_hour" || name == "seven_day" {
				_ = st.SetUsageLimit(name, store.UsageLimit{Utilization: w.Utilization, ResetsAt: w.ResetsAt, Status: w.Status, UpdatedAt: now})
			}
		}
	case ev.Type == "result":
		for model, u := range ev.ModelUsage {
			if err := st.AddUsage(store.UsageEvent{TS: now, Model: model, Input: u.In, Output: u.Out, CacheRead: u.CRead, CacheWrite: u.CWrit, Cost: u.Cost}); err != nil {
				log.Printf("recording usage: %v", err)
			}
		}
	}
}

type usageView struct {
	Now     int64                        `json:"now"`
	Model   string                       `json:"model"`
	Limits  map[string]*store.UsageLimit `json:"limits"`
	Session []store.UsageSum             `json:"session"` // since the current 5-hour window began
	Today   []store.UsageSum             `json:"today"`
	Week    []store.UsageSum             `json:"week"` // last 7 days
}

func (s *server) usageView() (usageView, error) {
	now := time.Now()
	v := usageView{Now: now.Unix(), Model: s.st.GetUsageModel(), Limits: map[string]*store.UsageLimit{}}
	for _, n := range []string{"five_hour", "seven_day"} {
		l, err := s.st.GetUsageLimit(n)
		if err != nil {
			return v, err
		}
		v.Limits[n] = l
	}
	sessionStart := now.Add(-5 * time.Hour).Unix()
	if l := v.Limits["five_hour"]; l != nil && l.ResetsAt > v.Now {
		sessionStart = l.ResetsAt - 5*3600
	}
	y, m, d := now.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Unix()
	var err error
	if v.Session, err = s.st.SumUsage(sessionStart); err != nil {
		return v, err
	}
	if v.Today, err = s.st.SumUsage(midnight); err != nil {
		return v, err
	}
	v.Week, err = s.st.SumUsage(now.Add(-7 * 24 * time.Hour).Unix())
	return v, err
}

// GET /api/usage
func (s *server) getUsage(w http.ResponseWriter, r *http.Request) {
	v, err := s.usageView()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

var (
	probeMu   sync.Mutex
	probeLast time.Time
)

// usageProbe makes one tiny Claude call so the CLI reports fresh limits. It is a variable so tests can stub it.
var usageProbe = func(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "one-piece-usage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--no-session-persistence", "--tools", "",
		"--setting-sources", "project", "--max-turns", "1", "--system-prompt", "Reply with just: ok"}
	_, err = runStream(ctx, args, dir, "ok", func(string) {}) // runStream feeds observeUsage
	return err
}

// POST /api/usage/refresh: limits only change when Claude runs, so this runs a tiny call (at most one per 20 s).
func (s *server) refreshUsage(w http.ResponseWriter, r *http.Request) {
	probeMu.Lock()
	if time.Since(probeLast) < 20*time.Second {
		probeMu.Unlock()
		s.getUsage(w, r)
		return
	}
	probeLast = time.Now()
	probeMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := usageProbe(ctx); err != nil {
		log.Printf("usage probe: %v", err)
	}
	s.getUsage(w, r)
}
