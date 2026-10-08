package sshconfig

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const maxIncludeDepth = 16

type Host struct {
	Alias         string
	HostName      string
	User          string
	Port          int
	IdentityFiles []string
	ProxyJump     string
}

type Config struct {
	blocks []block
	home   string
}

type kv struct {
	key, value string
}

// A Match block is kept but only "Match all" is evaluated; other criteria are
// treated as non-matching rather than guessed at.
type block struct {
	patterns []string
	matchAll bool
	isMatch  bool
	kvs      []kv
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
}

func Load(path string) (*Config, error) {
	home, _ := os.UserHomeDir()
	c := &Config{home: home}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := c.parse(f, filepath.Dir(path), 0); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func Parse(r io.Reader, sshDir, home string) (*Config, error) {
	c := &Config{home: home}
	return c, c.parse(r, sshDir, 0)
}

func (c *Config) parse(r io.Reader, sshDir string, depth int) error {
	if depth > maxIncludeDepth {
		return errors.New("include depth exceeded")
	}
	if depth == 0 {
		c.blocks = append(c.blocks, block{patterns: []string{"*"}})
	}
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		key, args := splitLine(sc.Text())
		if key == "" {
			continue
		}
		switch strings.ToLower(key) {
		case "host":
			c.blocks = append(c.blocks, block{patterns: args})
		case "match":
			all := len(args) == 1 && strings.EqualFold(args[0], "all")
			c.blocks = append(c.blocks, block{isMatch: true, matchAll: all})
		case "include":
			for _, a := range args {
				if err := c.include(a, sshDir, depth); err != nil {
					return err
				}
			}
		default:
			cur := &c.blocks[len(c.blocks)-1]
			cur.kvs = append(cur.kvs, kv{strings.ToLower(key), strings.Join(args, " ")})
		}
	}
	return sc.Err()
}

func (c *Config) include(pattern, sshDir string, depth int) error {
	p := expandTilde(pattern, c.home)
	if !filepath.IsAbs(p) {
		p = filepath.Join(sshDir, p)
	}
	matches, err := filepath.Glob(p)
	if err != nil {
		return err
	}
	// Content of an included file belongs to the enclosing block until the
	// file starts its own Host/Match section.
	enclosing := c.blocks[len(c.blocks)-1]
	for _, m := range matches {
		f, err := os.Open(m)
		if err != nil {
			continue
		}
		c.blocks = append(c.blocks, block{patterns: enclosing.patterns, isMatch: enclosing.isMatch, matchAll: enclosing.matchAll})
		err = c.parse(f, sshDir, depth+1)
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", m, err)
		}
	}
	c.blocks = append(c.blocks, block{patterns: enclosing.patterns, isMatch: enclosing.isMatch, matchAll: enclosing.matchAll})
	return nil
}

func splitLine(s string) (string, []string) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "#") {
		return "", nil
	}
	i := strings.IndexAny(s, " \t=")
	if i < 0 {
		return s, nil
	}
	key := s[:i]
	rest := strings.TrimLeft(s[i:], " \t")
	rest = strings.TrimPrefix(rest, "=")
	return key, splitArgs(strings.TrimSpace(rest))
}

func splitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inQuote, have := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			have = true
		case !inQuote && (r == ' ' || r == '\t'):
			if have {
				args = append(args, cur.String())
				cur.Reset()
				have = false
			}
		case !inQuote && r == '#':
			if have {
				args = append(args, cur.String())
			}
			return args
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if have {
		args = append(args, cur.String())
	}
	return args
}

func (c *Config) Aliases() []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range c.blocks {
		if b.isMatch {
			continue
		}
		for _, p := range b.patterns {
			if strings.ContainsAny(p, "*?!") || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func (c *Config) Resolve(alias string) Host {
	h := Host{Alias: alias}
	set := map[string]bool{}
	for _, b := range c.blocks {
		if !b.matches(alias) {
			continue
		}
		for _, e := range b.kvs {
			if e.key == "identityfile" {
				if !strings.EqualFold(e.value, "none") {
					h.IdentityFiles = append(h.IdentityFiles, e.value)
				}
				continue
			}
			if set[e.key] {
				continue
			}
			set[e.key] = true
			switch e.key {
			case "hostname":
				h.HostName = e.value
			case "user":
				h.User = e.value
			case "port":
				h.Port, _ = strconv.Atoi(e.value)
			case "proxyjump":
				if !strings.EqualFold(e.value, "none") {
					h.ProxyJump = e.value
				}
			}
		}
	}
	if h.HostName == "" {
		h.HostName = alias
	}
	h.HostName = strings.ReplaceAll(h.HostName, "%h", alias)
	if h.User == "" {
		h.User = localUser()
	}
	if h.Port == 0 {
		h.Port = 22
	}
	for i, f := range h.IdentityFiles {
		h.IdentityFiles[i] = expandTokens(f, c.home, h)
	}
	return h
}

func (b block) matches(alias string) bool {
	if b.isMatch {
		return b.matchAll
	}
	matched := false
	for _, p := range b.patterns {
		neg := strings.HasPrefix(p, "!")
		if globMatch(strings.TrimPrefix(p, "!"), alias) {
			if neg {
				return false
			}
			matched = true
		}
	}
	return matched
}

func globMatch(pattern, s string) bool {
	var re strings.Builder
	re.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			re.WriteString(".*")
		case '?':
			re.WriteString(".")
		default:
			re.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	re.WriteString("$")
	ok, _ := regexp.MatchString(re.String(), s)
	return ok
}

func expandTilde(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

func expandTokens(p, home string, h Host) string {
	r := strings.NewReplacer("%%", "%", "%d", home, "%u", localUser(), "%r", h.User, "%h", h.HostName)
	return filepath.FromSlash(expandTilde(r.Replace(p), home))
}

func localUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}
