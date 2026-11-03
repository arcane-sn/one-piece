package api

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

var errCanceled = errors.New("canceled")

// pickFolder opens the native macOS folder chooser and returns the chosen
// absolute path. It is a variable so tests can stub the dialog.
// ponytail: macOS-only (osascript); other platforms get an error and the UI says so.
var pickFolder = func() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "osascript",
		"-e", "tell current application to activate",
		"-e", `POSIX path of (choose folder with prompt "Choose a folder for this product")`).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "-128") { // user cancelled
			return "", errCanceled
		}
		return "", err
	}
	return strings.TrimRight(strings.TrimSpace(string(out)), "/"), nil
}

// POST /api/pick-folder → {"path": "..."}; path is "" when the dialog was cancelled.
func (s *server) pickFolder(w http.ResponseWriter, r *http.Request) {
	p, err := pickFolder()
	if err != nil && !errors.Is(err, errCanceled) {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": p})
}
