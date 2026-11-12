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

// Policy names the branches one ticket may touch in one repo.
type Policy struct {
	Feature string // feature/{ticket}
	Dev     string // feature/{ticket}-dev, cut from the shared dev branch and pushed for the PR
	Base    string // the branch Feature was cut from
	PRBase  string // the board's dev branch: the PR target
}

var (
	digitsRe  = regexp.MustCompile(`^[0-9]{1,4}$`)
	patternRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/*-]*$`)
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

func isStart(s string) bool { return ValidBranch(s) } // "main" or "origin/main"

// ValidateGit accepts only the git forms the branching flow needs; everything else is rejected,
// including every flag it does not list (an allow-list, not a deny-list).
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
			ok = (rest[1] == p.Feature || rest[1] == p.Dev) && ValidBranch(rest[1]) && isStart(rest[2])
		}
	case "merge":
		ok = len(rest) == 2 && rest[0] == "--no-edit" && rest[1] == p.Feature && ValidBranch(rest[1])
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

// Git validates args, applies the runtime guards, then runs git in repo.
func Git(repo string, args []string, p Policy) (string, error) {
	if err := ValidateGit(args, p); err != nil {
		return "", err
	}
	timeout := 30 * time.Second
	switch args[0] {
	case "push":
		timeout = 2 * time.Minute
	case "fetch":
		timeout = 2 * time.Minute
	case "switch", "merge":
		if st, err := gitRun(repo, timeout, "status", "--porcelain"); err != nil {
			return st, err
		} else if st != "" {
			return "", fmt.Errorf("uncommitted changes in %s", repo)
		}
		if args[0] == "merge" {
			cur, err := gitRun(repo, timeout, "rev-parse", "--abbrev-ref", "HEAD")
			if err != nil {
				return cur, err
			}
			if cur != p.Dev {
				return "", fmt.Errorf("merge only runs on %s, but the current branch is %s", p.Dev, cur)
			}
		}
		if args[0] == "switch" && args[1] == "-c" {
			if _, err := gitRun(repo, timeout, "rev-parse", "--verify", "--quiet", "refs/heads/"+args[2]); err == nil {
				return "", fmt.Errorf("branch %s already exists", args[2])
			}
		}
	}
	return gitRun(repo, timeout, args...)
}

// ValidateGH accepts only `pr create` (feature -dev branch into the PR base) and `pr view <dev branch>`.
func ValidateGH(args []string, p Policy) error {
	if len(args) >= 3 && args[0] == "pr" && args[1] == "view" && len(args) == 3 && args[2] == p.Dev {
		return nil
	}
	if len(args) == 10 && args[0] == "pr" && args[1] == "create" &&
		args[2] == "--base" && args[3] == p.PRBase && args[4] == "--head" && args[5] == p.Dev &&
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
