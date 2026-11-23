package qa

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Service is one process of the QA stack, configured by the owner.
type Service struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Folder        string `json:"folder"`
	Command       string `json:"command"`
	Port          int    `json:"port"`
	ReadyURL      string `json:"readyUrl"`
	ReadyTimeoutS int    `json:"readyTimeoutS"`
	External      bool   `json:"external"` // already running (started by the owner): never started or stopped by One Piece, only checked
}

// Config is a board's QA stack.
type Config struct {
	BaseURL         string    `json:"baseUrl"`
	LoginURL        string    `json:"loginUrl"`
	SessionCheckURL string    `json:"sessionCheckUrl"`
	AllowedHosts    []string  `json:"allowedHosts"`
	Headed          bool      `json:"headed"`
	Services        []Service `json:"services"`
}

// Configured reports whether the board has a QA stack (a base URL and at least one service).
func (c Config) Configured() bool { return c.BaseURL != "" && len(c.Services) > 0 }

// slugName turns any name into lowercase letters, digits and single dashes, at most 30 characters.
func slugName(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 30 {
		out = strings.TrimRight(out[:30], "-")
	}
	return out
}

var nameRe = regexp.MustCompile(`^[a-z0-9-]{1,30}$`)

func httpURL(label, s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("%s must be an http(s) URL", label)
	}
	return u, nil
}

// Validate checks and normalizes the config against the board's folders (symlinks resolved).
func (c *Config) Validate(boardFolders []string) error {
	base, err := httpURL("Base URL", c.BaseURL)
	if err != nil {
		return err
	}
	for _, f := range []struct {
		label string
		v     *string
	}{{"Login URL", &c.LoginURL}, {"Session check URL", &c.SessionCheckURL}} {
		if *f.v == "" {
			*f.v = c.BaseURL
		} else if _, err := httpURL(f.label, *f.v); err != nil {
			return err
		}
	}
	hosts := []string{}
	for _, h := range c.AllowedHosts {
		if strings.TrimSpace(h) == "" {
			continue
		}
		n, ok := NormalizeHost(h)
		if !ok {
			return fmt.Errorf("%q is not a host name (use example.test or *.example.test)", h)
		}
		hosts = append(hosts, n)
	}
	c.AllowedHosts = hosts
	for _, u := range []string{c.BaseURL, c.LoginURL, c.SessionCheckURL} {
		pu, _ := url.Parse(u)
		if !HostAllowed(pu.Host, hosts) {
			return fmt.Errorf("the host of %s must be in Allowed hosts", u)
		}
	}
	_ = base
	if len(c.Services) == 0 {
		return errors.New("add at least one service")
	}
	var roots []string
	for _, f := range boardFolders {
		if r, err := filepath.EvalSymlinks(f); err == nil {
			roots = append(roots, r)
		}
	}
	names, ports := map[string]bool{}, map[int]bool{}
	for i := range c.Services {
		s := &c.Services[i]
		s.Name = slugName(s.Name) // BASE_SERVICE, "Base Service" -> base-service (the name also names the log file)
		if !nameRe.MatchString(s.Name) {
			return errors.New("give every service a name")
		}
		if names[s.Name] {
			return fmt.Errorf("two services are both called %q; give them different names", s.Name)
		}
		names[s.Name] = true
		if s.Port < 1024 || s.Port > 65535 || ports[s.Port] {
			return fmt.Errorf("service %s: port must be 1024-65535 and unique", s.Name)
		}
		ports[s.Port] = true
		if !s.External && (strings.TrimSpace(s.Command) == "" || len(s.Command) > 1000) {
			return fmt.Errorf("service %s: a start command is required", s.Name)
		}
		ru, err := httpURL("Ready URL of "+s.Name, s.ReadyURL)
		if err != nil {
			return err
		}
		if !Loopback(ru.Hostname()) || ru.Port() != strconv.Itoa(s.Port) {
			return fmt.Errorf("service %s: the ready URL must be on localhost and on port %d", s.Name, s.Port)
		}
		if s.ReadyTimeoutS == 0 {
			s.ReadyTimeoutS = 120
		}
		if s.ReadyTimeoutS < 5 || s.ReadyTimeoutS > 900 {
			return fmt.Errorf("service %s: ready timeout must be 5-900 seconds", s.Name)
		}
		if s.External { // nothing to start: no command, no folder
			s.Command, s.Folder = "", ""
			continue
		}
		real, err := filepath.EvalSymlinks(s.Folder)
		if err != nil {
			return fmt.Errorf("service %s: folder not found", s.Name)
		}
		inside := false
		for _, r := range roots {
			if rel, err := filepath.Rel(r, real); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				inside = true
			}
		}
		if !inside {
			return fmt.Errorf("service %s: the folder must be inside one of the board's folders", s.Name)
		}
		s.Folder = real
	}
	return nil
}
