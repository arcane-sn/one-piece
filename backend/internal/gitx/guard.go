package gitx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Policy names the branches one ticket may touch in one repo, plus the saved state the guard needs.
// gitx never reads the database: the caller fills these in, which keeps every rule table-testable.
type Policy struct {
	Feature string // feature/{ticket}
	Dev     string // feature/{ticket}-dev, cut from the shared dev branch and pushed for the PR
	Base    string // the (release) branch Feature was cut from
	PRBase  string // the board's dev branch: the PR target

	Ident      string   // ticket identifier, e.g. MID-12 (stash messages must carry it)
	Mode       string   // "merge" (default) or "cherry-pick"
	Picked     []string // full SHAs already cherry-picked into Dev for this repo
	Integrated bool     // this repo's row is merged_dev, pushed or pr_open
	PushReady  bool     // EVERY via-dev repo of the ticket is integrated and none is blocked
	Direct     bool     // no -dev branch: the feature branch is pushed and the PR goes from it straight into Base
}

var (
	digitsRe   = regexp.MustCompile(`^[0-9]{1,4}$`)
	patternRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/*-]*$`)
	shaRe      = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	stashMsgRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._:/#-]{0,119}$`)
)

func reject(args []string) error {
	if len(args) == 0 {
		return errors.New("git: no command given")
	}
	s := strings.Join(args, " ")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return fmt.Errorf("git %s is not allowed here", s)
}

func isRef(s string) bool { return s == "HEAD" || ValidBranch(s) }

// isRange accepts "<ref>..<ref>".
func isRange(s string) bool {
	a, b, ok := strings.Cut(s, "..")
	return ok && isRef(a) && isRef(b)
}

// startOK pins where a new branch may start: Feature at the base, Dev at the PR base (local or origin/).
func startOK(branch, start string, p Policy) bool {
	switch branch {
	case p.Feature:
		return p.Base != "" && (start == p.Base || start == "origin/"+p.Base)
	case p.Dev:
		return p.PRBase != "" && (start == p.PRBase || start == "origin/"+p.PRBase)
	}
	return false
}

// baseMergeable: the base may be merged into Feature only when it is a real release branch,
// never the shared dev branch or a -dev branch.
func baseMergeable(p Policy) bool {
	return p.Base != "" && p.Base != p.PRBase && p.Base != "dev" && !strings.HasSuffix(p.Base, "-dev") && ValidBranch(p.Base)
}

// hasToken reports whether s contains ident as a whole token (MID-1 is not inside MID-12 or MID-1-dev).
func hasToken(s, ident string) bool {
	if ident == "" {
		return false
	}
	return regexp.MustCompile(`(^|[^A-Za-z0-9-])` + regexp.QuoteMeta(ident) + `($|[^A-Za-z0-9-])`).MatchString(s)
}

// ValidateGit accepts only the git forms the branching flow needs; everything else is rejected,
// including every flag it does not list (an allow-list, not a deny-list). State-dependent rules
// (current branch, clean tree, commit ranges, push gate) are enforced by GitX.
func ValidateGit(args []string, p Policy) error {
	if len(args) == 0 {
		return reject(args)
	}
	rest := args[1:]
	ok := false
	switch args[0] {
	case "status":
		ok = len(rest) == 0 || (len(rest) == 1 && (rest[0] == "-s" || rest[0] == "--short" || rest[0] == "--porcelain" || rest[0] == "-b"))
	case "fetch":
		switch len(rest) {
		case 0:
			ok = true
		case 1:
			ok = rest[0] == "--prune" || rest[0] == "origin"
		case 2:
			ok = rest[0] == "--prune" && rest[1] == "origin"
		}
	case "branch":
		switch {
		case len(rest) == 1:
			ok = rest[0] == "--show-current" || rest[0] == "-r" || rest[0] == "--list"
		case len(rest) == 2:
			ok = rest[0] == "--list" && patternRe.MatchString(rest[1])
		}
	case "rev-parse":
		ok = (len(rest) == 2 && rest[0] == "--abbrev-ref" && rest[1] == "HEAD") ||
			(len(rest) == 2 && rest[0] == "--verify" && isRef(rest[1]))
	case "switch":
		switch {
		case len(rest) == 1:
			ok = ValidBranch(rest[0])
		case len(rest) == 3 && rest[0] == "-c":
			ok = ValidBranch(rest[1]) && startOK(rest[1], rest[2], p)
		}
	case "checkout":
		switch {
		case len(rest) == 1:
			ok = ValidBranch(rest[0])
		case len(rest) == 3 && rest[0] == "-b":
			ok = ValidBranch(rest[1]) && startOK(rest[1], rest[2], p)
		}
	case "pull":
		ok = len(rest) == 1 && rest[0] == "--ff-only"
	case "stash":
		switch {
		case len(rest) == 1:
			ok = rest[0] == "list" || rest[0] == "pop"
		case len(rest) == 4:
			ok = rest[0] == "push" && rest[1] == "-u" && rest[2] == "-m" && stashMsgRe.MatchString(rest[3]) && hasToken(rest[3], p.Ident)
		}
	case "merge":
		switch {
		case len(rest) == 1:
			ok = rest[0] == "--abort"
		case len(rest) == 2 && rest[0] == "--no-edit" && ValidBranch(rest[1]):
			x := rest[1]
			ok = x == p.Feature || (p.PRBase != "" && x == "origin/"+p.PRBase) ||
				(baseMergeable(p) && (x == p.Base || x == "origin/"+p.Base))
		}
	case "cherry-pick":
		switch {
		case len(rest) == 1:
			ok = rest[0] == "--abort"
		case len(rest) >= 2 && len(rest) <= 51 && rest[0] == "-x":
			ok = true
			for _, s := range rest[1:] {
				ok = ok && shaRe.MatchString(s)
			}
		}
	case "push":
		if len(rest) > 0 && rest[0] == "-u" {
			rest = rest[1:]
		}
		ok = len(rest) == 2 && rest[0] == "origin" && ValidBranch(rest[1]) && (rest[1] == p.Feature || rest[1] == p.Dev) &&
			!isProtected(rest[1], p)
	case "log":
		ok = true
		for i := 0; i < len(rest) && ok; i++ {
			switch {
			case rest[i] == "--oneline":
			case rest[i] == "-n" && i+1 < len(rest) && digitsRe.MatchString(rest[i+1]):
				i++
			case isRange(rest[i]):
			default:
				ok = false
			}
		}
	case "diff":
		ok = len(rest) == 2 && rest[0] == "--stat" && isRange(rest[1])
	}
	if !ok {
		return reject(args)
	}
	return nil
}

func isProtected(branch string, p Policy) bool {
	switch branch {
	case "dev", "main", "master", p.Base, p.PRBase:
		return true
	}
	return false
}

func gitRun(repo string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		if ctx.Err() != nil {
			return s, fmt.Errorf("git %s timed out", args[0])
		}
		return s, fmt.Errorf("git %s failed: %v\n%s", args[0], err, s)
	}
	return s, nil
}

// Result is what a guarded git call produced, plus facts the caller must record (Claude never writes them).
type Result struct {
	Out         string
	Picked      []string // full SHAs cherry-picked successfully
	Stash       string   // sha of a stash this call created
	StashBranch string
	StashMsg    string
	Popped      string // sha of the stash this call popped
}

func refExists(repo, ref string) bool {
	_, err := gitRun(repo, 30*time.Second, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

func currentBranch(repo string) (string, error) {
	cur, err := gitRun(repo, 30*time.Second, "rev-parse", "--abbrev-ref", "HEAD")
	return cur, err
}

func cleanTree(repo string) error {
	st, err := gitRun(repo, 30*time.Second, "status", "--porcelain")
	if err != nil {
		return err
	}
	if st != "" {
		return fmt.Errorf("uncommitted changes in %s", repo)
	}
	return nil
}

// baseRef is the ref commits are counted from: origin/<Base> when it exists, else <Base>.
func baseRef(repo string, p Policy) string {
	if refExists(repo, "refs/remotes/origin/"+p.Base) {
		return "origin/" + p.Base
	}
	return p.Base
}

// Commit is one feature commit that is a candidate for cherry-picking.
type Commit struct{ SHA, Subject string }

// ownCommits lists the non-merge commits in <base>..<Feature>, oldest first.
func ownCommits(repo string, p Policy) ([]Commit, error) {
	out, err := gitRun(repo, 30*time.Second, "log", "--no-merges", "--reverse", "--format=%H%x09%s", baseRef(repo, p)+".."+p.Feature)
	if err != nil {
		return nil, err
	}
	var cs []Commit
	for _, l := range strings.Split(out, "\n") {
		if sha, sub, ok := strings.Cut(l, "\t"); ok {
			cs = append(cs, Commit{sha, sub})
		}
	}
	return cs, nil
}

// PendingPicks lists the ticket's own commits not yet cherry-picked, oldest first.
func PendingPicks(repo string, p Policy) ([]Commit, error) {
	all, err := ownCommits(repo, p)
	if err != nil {
		return nil, err
	}
	done := map[string]bool{}
	for _, s := range p.Picked {
		done[s] = true
	}
	var out []Commit
	for _, c := range all {
		if !done[c.SHA] {
			out = append(out, c)
		}
	}
	return out, nil
}

// Git validates args, applies the runtime guards, then runs git in repo.
func Git(repo string, args []string, p Policy) (string, error) {
	r, err := GitX(repo, args, p)
	return r.Out, err
}

// GitX is Git plus the facts the caller must record afterwards.
func GitX(repo string, args []string, p Policy) (Result, error) {
	var res Result
	if err := ValidateGit(args, p); err != nil {
		return res, err
	}
	timeout := 30 * time.Second
	if args[0] == "push" || args[0] == "fetch" || args[0] == "pull" {
		timeout = 2 * time.Minute
	}
	rest := args[1:]
	fail := func(f string, a ...any) (Result, error) { return res, fmt.Errorf(f, a...) }
	run := args
	var cur string
	needsBranch := func() error {
		var err error
		cur, err = currentBranch(repo)
		return err
	}
	onTicket := func() error {
		if err := needsBranch(); err != nil {
			return err
		}
		if cur != p.Feature && cur != p.Dev {
			return fmt.Errorf("%s only runs on %s or %s, but the current branch is %s", args[0], p.Feature, p.Dev, cur)
		}
		return nil
	}
	isAbort := (args[0] == "merge" || args[0] == "cherry-pick") && rest[0] == "--abort"
	switch args[0] {
	case "switch", "checkout", "pull", "merge", "cherry-pick":
		if !isAbort {
			if err := cleanTree(repo); err != nil {
				return res, err
			}
		}
	case "stash":
		if rest[0] == "pop" {
			if err := cleanTree(repo); err != nil {
				return res, err
			}
		}
	}
	switch args[0] {
	case "switch":
		if rest[0] == "-c" {
			if refExists(repo, "refs/heads/"+rest[1]) {
				return fail("branch %s already exists", rest[1])
			}
			if !refExists(repo, "refs/heads/"+rest[2]) && !refExists(repo, "refs/remotes/"+rest[2]) {
				return fail("start point %s is not a branch", rest[2])
			}
			run = []string{"switch", "--no-track", "-c", rest[1], rest[2]}
		}
	case "checkout":
		if rest[0] == "-b" {
			if refExists(repo, "refs/heads/"+rest[1]) {
				return fail("branch %s already exists", rest[1])
			}
			if !refExists(repo, "refs/heads/"+rest[2]) && !refExists(repo, "refs/remotes/"+rest[2]) {
				return fail("start point %s is not a branch", rest[2])
			}
			run = []string{"checkout", "--no-track", "-b", rest[1], rest[2]}
		} else {
			if !refExists(repo, "refs/heads/"+rest[0]) && !refExists(repo, "refs/remotes/origin/"+rest[0]) {
				return fail("%s is not a branch (checkout only switches branches)", rest[0])
			}
			run = []string{"checkout", rest[0], "--"}
		}
	case "pull":
		if err := onTicket(); err != nil {
			return res, err
		}
		up, err := gitRun(repo, timeout, "rev-parse", "--abbrev-ref", "@{upstream}")
		if err != nil || up != "origin/"+cur {
			return fail("%s does not track origin/%s; push -u it first", cur, cur)
		}
	case "stash":
		if rest[0] == "pop" {
			if err := needsBranch(); err != nil {
				return res, err
			}
			top, err := gitRun(repo, timeout, "stash", "list", "-1", "--format=%gs")
			if err != nil || top == "" {
				return fail("there is no stash to pop")
			}
			if !hasToken(top, p.Ident) {
				return fail("the top stash does not belong to %s", p.Ident)
			}
			if !strings.HasPrefix(top, "On "+cur+":") {
				return fail("that stash was made on another branch than %s", cur)
			}
			res.Popped, _ = gitRun(repo, timeout, "rev-parse", "--verify", "refs/stash")
		}
		if rest[0] == "push" {
			before, _ := gitRun(repo, timeout, "rev-parse", "-q", "--verify", "refs/stash")
			if err := needsBranch(); err != nil {
				return res, err
			}
			out, err := gitRun(repo, timeout, args...)
			res.Out = out
			if err != nil {
				return res, err
			}
			if after, _ := gitRun(repo, timeout, "rev-parse", "-q", "--verify", "refs/stash"); after != "" && after != before {
				res.Stash, res.StashBranch, res.StashMsg = after, cur, rest[3]
			}
			return res, nil
		}
	case "merge":
		if err := onTicket(); err != nil {
			return res, err
		}
		if isAbort {
			break
		}
		x := rest[1]
		switch cur {
		case p.Dev:
			if x == p.Feature && p.Mode == "cherry-pick" {
				return fail("this repo integrates by cherry-pick; merging the feature is not allowed (change the mode first)")
			}
			if x != p.Feature && x != "origin/"+p.PRBase {
				return fail("on %s only %s or origin/%s may be merged", p.Dev, p.Feature, p.PRBase)
			}
		case p.Feature:
			if x != p.Base && x != "origin/"+p.Base {
				return fail("on %s only the base branch %s may be merged", p.Feature, p.Base)
			}
		}
	case "cherry-pick":
		if err := needsBranch(); err != nil {
			return res, err
		}
		if cur != p.Dev {
			return fail("cherry-pick only runs on %s, but the current branch is %s", p.Dev, cur)
		}
		if isAbort {
			break
		}
		if p.Mode != "cherry-pick" {
			return fail("this repo integrates by merge; set the mode to cherry-pick first")
		}
		own, err := ownCommits(repo, p)
		if err != nil {
			return res, err
		}
		pos := map[string]int{}
		for i, c := range own {
			pos[c.SHA] = i
		}
		done := map[string]bool{}
		for _, s := range p.Picked {
			done[s] = true
		}
		last := -1
		for _, s := range rest[1:] {
			full, err := gitRun(repo, timeout, "rev-parse", "--verify", "--quiet", s+"^{commit}")
			if err != nil {
				return fail("%s is not a commit", s)
			}
			i, in := pos[full]
			switch {
			case !in:
				return fail("%s is not one of the ticket's own commits (non-merge commits in %s..%s)", s, baseRef(repo, p), p.Feature)
			case done[full]:
				return fail("%s was already cherry-picked", s)
			case i <= last:
				return fail("give the commits oldest first, without repeats")
			}
			last = i
			res.Picked = append(res.Picked, full)
		}
	case "push":
		b := rest[len(rest)-1]
		if p.Direct && b == p.Dev {
			return fail("this repo goes straight from its feature branch: there is no -dev branch to push")
		}
		if !p.Direct && ((b == p.Dev && !p.PushReady) || (b == p.Feature && p.Integrated && !p.PushReady)) {
			return fail("nothing is pushed until every repo of this ticket has integrated cleanly (and none is blocked)")
		}
	}
	out, err := gitRun(repo, timeout, run...)
	res.Out = out
	if err != nil {
		res.Picked, res.Popped = nil, ""
		return res, err
	}
	return res, nil
}

// RemoteKind classifies the origin remote: "github", "other" or "none". Variable so tests can stub it.
var RemoteKind = func(repo string) string {
	u, err := gitRun(repo, 15*time.Second, "remote", "get-url", "origin")
	if err != nil || u == "" {
		return "none"
	}
	return remoteKindOf(u)
}

func remoteKindOf(u string) string {
	host := ""
	switch {
	case strings.Contains(u, "://"):
		rest := u[strings.Index(u, "://")+3:]
		if i := strings.IndexAny(rest, "/"); i >= 0 {
			rest = rest[:i]
		}
		if i := strings.LastIndex(rest, "@"); i >= 0 {
			rest = rest[i+1:]
		}
		host, _, _ = strings.Cut(rest, ":")
	case strings.Contains(u, "@") && strings.Contains(u, ":"):
		rest := u[strings.LastIndex(u, "@")+1:]
		host, _, _ = strings.Cut(rest, ":")
	}
	if strings.EqualFold(host, "github.com") {
		return "github"
	}
	return "other"
}

// ValidateGH accepts only `pr create` and `pr view <head>`: feature -dev branch into the PR base, or for a
// Direct repo the feature branch into its own base branch.
func ValidateGH(args []string, p Policy) error {
	base, head := p.PRBase, p.Dev
	if p.Direct {
		base, head = p.Base, p.Feature
	}
	if len(args) >= 3 && args[0] == "pr" && args[1] == "view" && len(args) == 3 && args[2] == head {
		return nil
	}
	if len(args) == 10 && args[0] == "pr" && args[1] == "create" &&
		args[2] == "--base" && args[3] == base && args[4] == "--head" && args[5] == head &&
		args[6] == "--title" && len(args[7]) <= 200 && args[8] == "--body" && len(args[9]) <= 10000 {
		return nil
	}
	s := strings.Join(args, " ")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return fmt.Errorf("gh %s is not allowed here", s)
}

// GH validates args, then runs the GitHub CLI in repo.
func GH(repo string, args []string, p Policy) (string, error) {
	if err := ValidateGH(args, p); err != nil {
		return "", err
	}
	if k := RemoteKind(repo); k != "github" {
		return "", fmt.Errorf("this repo's remote is not on GitHub (%s); skip gh and report the pushed branches instead", k)
	}
	if args[1] == "create" && !p.Direct && !p.Integrated {
		return "", errors.New("gh pr create needs the repo to be integrated (merged_dev) and pushed first")
	}
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", errors.New("gh is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("gh failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
