// Package gitrepo asks git about the folder a project is based on: the
// repository root, its worktrees, and making a folder a repository. Every
// answer comes from the git binary, so what it says is what git says.
package gitrepo

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Worktree is one worktree of a repository: its folder and the branch it
// has checked out, "" when detached.
type Worktree struct {
	Path   string
	Branch string
}

// Root is the top-level folder of the repository holding dir, as git
// reports it. A folder that is not inside a repository is an error.
func Root(dir string) (string, error) {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Worktrees lists the repository's worktrees, the main one first, from
// git worktree list --porcelain.
func Worktrees(root string) ([]Worktree, error) {
	out, err := run(root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var all []Worktree
	var cur *Worktree
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			all = append(all, Worktree{Path: strings.TrimPrefix(line, "worktree ")})
			cur = &all[len(all)-1]
		case strings.HasPrefix(line, "branch ") && cur != nil:
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	return all, nil
}

// Init makes dir a repository with git init, creating the folder when it
// does not exist. A folder already inside a repository is left alone.
func Init(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := Root(dir); err == nil {
		return nil
	}
	_, err := run(dir, "init", "-q")
	return err
}

// run is git with args in dir; a failure carries git's own message.
func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			msg := strings.TrimSpace(errb.String())
			if msg == "" {
				msg = err.Error()
			}
			return "", fmt.Errorf("git %s: %s", args[0], msg)
		}
		return "", fmt.Errorf("git: %w", err)
	}
	return out.String(), nil
}
