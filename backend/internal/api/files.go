package api

import (
	"fmt"
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

// dotenv templates hold no secrets, so Claude may read them (".env.example" yes, ".env" no).
var envTemplate = regexp.MustCompile(`(?i)\.(example|sample|template|dist|defaults?)$`)

// secretDenyRules returns --disallowedTools Read rules that block dotenv files under dirs
// (Claude's Read deny also covers Grep and Glob; verified with the real CLI):
//   - every file named exactly ".env", at any depth, present or created later (one glob per dir)
//   - every other existing ".env.*" / "*.env" file, except templates like ".env.example"
//     (a glob cannot exclude templates, since deny beats allow, so these are listed one by one)
func secretDenyRules(dirs []string) []string {
	var rules []string
	seen := map[string]bool{}
	add := func(r string) {
		if !seen[r] {
			seen[r] = true
			rules = append(rules, r)
		}
	}
	for _, root := range dirs {
		root = filepath.Clean(root)
		if !filepath.IsAbs(root) {
			continue
		}
		add("Read(/" + root + "/**/.env)") // "//<abs>" = absolute path in Claude's rule syntax
		scanned := 0
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if name == ".git" || skipDirs[name] {
					return filepath.SkipDir
				}
				return nil
			}
			if scanned++; scanned > maxFileScan {
				return filepath.SkipAll
			}
			if name != ".env" && isSecretEnv(name) {
				add("Read(/" + p + ")")
			}
			return nil
		})
	}
	return rules
}

// denyArgs is the --disallowedTools argument list for secretDenyRules, or nil.
func denyArgs(dirs []string) []string {
	r := secretDenyRules(dirs)
	if len(r) == 0 {
		return nil
	}
	return append([]string{"--disallowedTools"}, r...)
}

// isSecretEnv reports whether a file name is a dotenv file that may hold secrets.
func isSecretEnv(name string) bool {
	return name == ".env" || ((strings.HasPrefix(name, ".env.") || strings.HasSuffix(name, ".env")) && !envTemplate.MatchString(name))
}

// mentionContext describes @-mentioned paths so Claude can start from them: folders list their
// entries, files give their size. File contents are never inlined (Claude opens what it needs), and
// secret files are named as blocked.
func mentionContext(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		switch {
		case isSecretEnv(info.Name()):
			b.WriteString("- " + p + "  (secret file, blocked)\n")
		case info.IsDir():
			ents, _ := os.ReadDir(p)
			var names []string
			for _, e := range ents {
				n := e.Name()
				if strings.HasPrefix(n, ".") || skipDirs[n] || isSecretEnv(n) {
					continue
				}
				if e.IsDir() {
					n += "/"
				}
				names = append(names, n)
			}
			more := ""
			if len(names) > 40 {
				more, names = fmt.Sprintf(", +%d more", len(names)-40), names[:40]
			}
			fmt.Fprintf(&b, "- %s  (folder: %s%s)\n", p, strings.Join(names, ", "), more)
		default:
			fmt.Fprintf(&b, "- %s  (file, %d bytes)\n", p, info.Size())
		}
	}
	return b.String()
}
