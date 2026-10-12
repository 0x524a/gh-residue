// Package input turns rewrite artefacts into the list of old commits to probe.
package input

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/0x524a/gh-residue/internal/model"
)

var sha40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

const nullSHA = "0000000000000000000000000000000000000000"

// ParseSHAList reads one full 40-char SHA per line. Blank lines and #-comments
// are skipped. Short or malformed SHAs are an error: ambiguity would hide findings.
func ParseSHAList(r io.Reader) ([]model.OldCommit, error) {
	var out []model.OldCommit
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.ToLower(strings.TrimSpace(sc.Text()))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !sha40.MatchString(line) {
			return nil, fmt.Errorf("line %d: %q is not a full 40-character SHA", n, line)
		}
		if !seen[line] {
			seen[line] = true
			out = append(out, model.OldCommit{SHA: line})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ParseCommitMap reads git-filter-repo's commit-map ("old new" per line, with a
// leading "old new" header). Only commits whose SHA actually changed are returned;
// unchanged and pruned (null new SHA) entries are handled as documented below.
// Pruned commits (new == null) were removed entirely and are still old exposure.
// The order is whatever filter-repo wrote, which is sorted by old SHA, NOT
// processing order: do not infer the first changed commit from it.
func ParseCommitMap(r io.Reader) ([]model.OldCommit, error) {
	var out []model.OldCommit
	sc := bufio.NewScanner(r)
	first := true
	for n := 1; sc.Scan(); n++ {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		if first {
			first = false
			if f[0] == "old" && len(f) == 2 && f[1] == "new" {
				continue
			}
		}
		if len(f) != 2 || !sha40.MatchString(f[0]) || !sha40.MatchString(f[1]) {
			return nil, fmt.Errorf("commit-map line %d: expected two 40-character SHAs", n)
		}
		if f[0] == f[1] {
			continue // commit unchanged by the rewrite
		}
		c := model.OldCommit{SHA: f[0]}
		if f[1] != nullSHA {
			c.NewSHA = f[1]
		}
		out = append(out, c)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Load resolves a path: a directory is treated as a filter-repo working
// directory (.git/filter-repo/commit-map, or filter-repo/commit-map for a bare
// repo); a file is a SHA list. It returns the commits and a source label.
func Load(path string) ([]model.OldCommit, string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if !st.IsDir() {
		f, err := os.Open(path)
		if err != nil {
			return nil, "", err
		}
		defer f.Close()
		c, err := ParseSHAList(f)
		return c, "sha-list", err
	}
	for _, p := range []string{
		filepath.Join(path, ".git", "filter-repo", "commit-map"),
		filepath.Join(path, "filter-repo", "commit-map"),
	} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()
		c, err := ParseCommitMap(f)
		return c, "filter-repo", err
	}
	return nil, "", fmt.Errorf("no filter-repo commit-map found under %s", path)
}

// LoadFirstChanged reads filter-repo's own record of the first changed commits
// (.git/filter-repo/first-changed-commits: "<changed commit> <undeleted ancestor>"
// per line; the first column is what its "NOTE: First Changed Commit(s)" prints).
// It returns nil, nil when the file does not exist, and never guesses.
func LoadFirstChanged(dir string) ([]string, error) {
	for _, p := range []string{
		filepath.Join(dir, ".git", "filter-repo", "first-changed-commits"),
		filepath.Join(dir, "filter-repo", "first-changed-commits"),
	} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()
		return parseFirstChanged(f)
	}
	return nil, nil
}

func parseFirstChanged(r io.Reader) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		if !sha40.MatchString(f[0]) {
			return nil, fmt.Errorf("first-changed-commits line %d: %q is not a full 40-character SHA", n, f[0])
		}
		out = append(out, f[0])
	}
	return out, sc.Err()
}
