package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

const maxUploadBytes = 10 << 20 // decoded image size

var uploadName = regexp.MustCompile(`^[0-9a-f]{64}\.(png|jpg|gif|webp)$`)

var uploadExt = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp"}

// uploadsDir holds pasted images (next to the database, ignored by git).
func (s *server) uploadsDir() string { return filepath.Join(s.st.Dir, "uploads") }

// POST /api/uploads {"data":"data:image/png;base64,..."} → {"url":"/api/uploads/<sha256>.png"}
// Only PNG, JPEG, GIF and WebP are kept (no SVG: it can carry scripts); the type is sniffed, not trusted.
func (s *server) uploadImage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Data string `json:"data"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	b64 := in.Data
	if i := strings.Index(b64, ","); strings.HasPrefix(b64, "data:") && i > 0 {
		b64 = b64[i+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) == 0 {
		writeErr(w, store.ValidationError{Msg: "not a valid image"})
		return
	}
	if len(raw) > maxUploadBytes {
		writeErr(w, store.ValidationError{Msg: "image is larger than 10 MB"})
		return
	}
	ext, ok := uploadExt[http.DetectContentType(raw)]
	if !ok {
		writeErr(w, store.ValidationError{Msg: "only PNG, JPEG, GIF and WebP images are supported"})
		return
	}
	sum := sha256.Sum256(raw)
	name := hex.EncodeToString(sum[:]) + "." + ext
	if err := os.MkdirAll(s.uploadsDir(), 0o755); err != nil {
		writeErr(w, err)
		return
	}
	dst := filepath.Join(s.uploadsDir(), name)
	if _, err := os.Stat(dst); err != nil { // same image twice is stored once
		if err := os.WriteFile(dst, raw, 0o644); err != nil {
			writeErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": "/api/uploads/" + name})
}

// GET /api/uploads/{name}
func (s *server) getUpload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !uploadName.MatchString(name) {
		writeErr(w, store.ErrNotFound)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(s.uploadsDir(), name))
}
