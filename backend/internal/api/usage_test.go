package api

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func TestObserveUsageRecordsLimitsModelAndTokens(t *testing.T) {
	s, _, ck, _ := bsSetup(t)
	reset := time.Now().Add(2 * time.Hour).Unix()
	observeUsage([]byte(`{"type":"system","subtype":"init","model":"claude-sonnet-5-5"}`))
	observeUsage([]byte(`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"five_hour":{"utilization":0.25,"resetsAt":` + strconv.FormatInt(reset, 10) + `,"status":"allowed"},"seven_day":{"utilization":0.66,"resetsAt":` + strconv.FormatInt(reset+86400, 10) + `,"status":"allowed"}}}}`))
	observeUsage([]byte(`{"type":"result","modelUsage":{"claude-sonnet-5-5":{"inputTokens":10,"outputTokens":5,"cacheReadInputTokens":100,"cacheCreationInputTokens":50,"costUSD":0.02}}}`))
	observeUsage([]byte(`{"type":"assistant","message":{}}`)) // ignored
	resp, body := s.req(t, "GET", "/api/usage", nil, ck)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var v usageView
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	if v.Model != "claude-sonnet-5-5" || v.Limits["five_hour"] == nil || v.Limits["five_hour"].Utilization != 0.25 || v.Limits["seven_day"].Utilization != 0.66 {
		t.Fatalf("view = %+v", v)
	}
	if len(v.Today) != 1 || v.Today[0].Input != 10 || v.Today[0].CacheRead != 100 || v.Today[0].Cost != 0.02 || len(v.Session) != 1 || len(v.Week) != 1 {
		t.Fatalf("sums = today %+v session %+v", v.Today, v.Session)
	}
}

func TestUsageRefreshRunsProbeAtMostOncePer20s(t *testing.T) {
	s, _, ck, _ := bsSetup(t)
	calls := 0
	old := usageProbe
	usageProbe = func(context.Context) error { calls++; return nil }
	probeLast = time.Time{}
	defer func() { usageProbe = old; probeLast = time.Time{} }()
	for i := 0; i < 2; i++ {
		if resp, _ := s.req(t, "POST", "/api/usage/refresh", map[string]any{}, ck); resp.StatusCode != 200 {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1", calls)
	}
}
