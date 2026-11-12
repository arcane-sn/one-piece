package api

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/arcane-sn/one-piece/backend/internal/gitx"
)

// Directories never worth offering when picking code to mention.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
	"__pycache__": true, "coverage": true, "out": true,
}

const (
	maxFileHits  = 30
	maxFileScan  = 30000 // entries looked at per request
	maxFileDepth = 8
)

type fileHit struct {
	Path  string `json:"path"` // "<folder name>/relative/path"
	IsDir bool   `json:"isDir"`
	// IsRepo marks a folder that is a git repository; the Discuss session branches per repo.
	IsRepo bool `json:"isRepo"`
}

// GET /api/boards/{id}/files?q=text → files and folders of the board's folders whose path
// contains q (an empty q lists the top level), for the "@" picker in prompts.
func (s *server) listFiles(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.st.GetBoard(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var hits []fileHit
	scanned := 0
	for _, root := range b.Folders {
		base := filepath.Base(root)
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			if rel == "." {
				if q == "" || strings.Contains(strings.ToLower(base), q) {
					hits = append(hits, fileHit{Path: base, IsDir: true, IsRepo: gitx.IsRepo(root)})
				}
				return nil
			}
			name := d.Name()
			depth := strings.Count(rel, string(filepath.Separator))
			if strings.HasPrefix(name, ".") || (d.IsDir() && skipDirs[name]) || depth >= maxFileDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if scanned++; scanned > maxFileScan {
				return filepath.SkipAll
			}
			shown := base + "/" + filepath.ToSlash(rel)
			if q == "" {
				if depth == 0 { // top level only
					hits = append(hits, fileHit{Path: shown, IsDir: d.IsDir(), IsRepo: d.IsDir() && gitx.IsRepo(p)})
				}
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.Contains(strings.ToLower(shown), q) {
				hits = append(hits, fileHit{Path: shown, IsDir: d.IsDir(), IsRepo: d.IsDir() && gitx.IsRepo(p)})
			}
			return nil
		})
	}
	rank := func(h fileHit) int { // repos first, then name match beats path match, then shorter paths
		switch {
		case h.IsRepo:
			return 0
		case strings.HasPrefix(strings.ToLower(filepath.Base(h.Path)), q):
			return 1
		}
		return 2
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if ri, rj := rank(hits[i]), rank(hits[j]); ri != rj {
			return ri < rj
		}
		if len(hits[i].Path) != len(hits[j].Path) {
			return len(hits[i].Path) < len(hits[j].Path)
		}
		return hits[i].Path < hits[j].Path
	})
	if len(hits) > maxFileHits {
		hits = hits[:maxFileHits]
	}
	if hits == nil {
		hits = []fileHit{}
	}
	writeJSON(w, http.StatusOK, hits)
}

var mentionRe = regexp.MustCompile(`@([^\s@]+)`)

// mentionedPaths resolves "@<folder name>/sub/path" mentions in text to absolute paths inside the
// board's folders, and returns those paths plus the board folders that contain them. Mentions that
// do not exist or point outside the folders are ignored.
func mentionedPaths(folders []string, text string) (paths, roots []string) {
	seen := map[string]bool{}
	rootSeen := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		ref := strings.TrimRight(m[1], ".,;:!?)\"'`")
		first, rest, _ := strings.Cut(ref, "/")
		for _, root := range folders {
			if filepath.Base(root) != first {
				continue
			}
			p := filepath.Join(root, filepath.FromSlash(rest))
			if rel, err := filepath.Rel(root, p); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				break
			}
			if _, err := os.Stat(p); err != nil {
				break
			}
			if !seen[p] && len(paths) < 10 {
				seen[p] = true
				paths = append(paths, p)
			}
			if !rootSeen[root] {
				rootSeen[root] = true
				roots = append(roots, root)
			}
			break
		}
	}
	return paths, roots
}

// repoMentions resolves the same "@<folder name>/…" mentions as mentionedPaths to git repository
// roots. A mention that exists but is not inside a repo goes to notRepos (absolute path).
// Both lists are deduplicated; mentions outside the folders or that do not exist are ignored.
func repoMentions(folders []string, text string) (repos, notRepos []string) {
	seenR, seenN := map[string]bool{}, map[string]bool{}
	paths, _ := mentionedPaths(folders, text)
	for _, p := range paths {
		var root string
		for _, f := range folders {
			if rel, err := filepath.Rel(f, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				root = f
				break
			}
		}
		if rr, err := gitx.RepoRootWithin(root, p); err == nil {
			if !seenR[rr] {
				seenR[rr] = true
				repos = append(repos, rr)
			}
		} else if !seenN[p] {
			seenN[p] = true
			notRepos = append(notRepos, p)
		}
	}
	return repos, notRepos
}
