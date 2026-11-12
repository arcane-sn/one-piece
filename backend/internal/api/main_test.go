package api

import (
	"context"
	"errors"
	"os"
	"testing"
)

// Tests must never start the real `claude` CLI: reviews run on every card save.
func TestMain(m *testing.M) {
	streamClaude = func(context.Context, string, string, []string, func(string)) (string, error) {
		return "", errors.New("claude is stubbed in tests")
	}
	runClaude = func(context.Context, string, string) (string, error) {
		return "", errors.New("claude is stubbed in tests")
	}
	runBrainstorm = func(context.Context, brainstormRun, func(string)) (string, error) {
		return "", errors.New("claude is stubbed in tests")
	}
	agentRun = func(context.Context, string, []string, string, string, func(string)) (string, error) {
		return "", errors.New("claude is stubbed in tests")
	}
	os.Exit(m.Run())
}
