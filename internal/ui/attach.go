package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/complete"
)

// attachPickedMsg carries the files the OS dialog returned for a form's
// Attach field. A form that is no longer the open one ignores it.
type attachPickedMsg struct {
	form  *form
	paths []string
	err   error
}

// pickCommand is the platform's native multi-file dialog, printing one path
// per line, or nothing and an error when it is cancelled.
func pickCommand(ctx context.Context) (*exec.Cmd, error) {
	if runtime.GOOS == "darwin" {
		return exec.CommandContext(ctx, "osascript",
			"-e", "set fs to choose file with multiple selections allowed",
			"-e", `set out to ""`,
			"-e", "repeat with f in fs",
			"-e", "set out to out & POSIX path of f & linefeed",
			"-e", "end repeat",
			"-e", "return out"), nil
	}
	if p, err := exec.LookPath("zenity"); err == nil {
		return exec.CommandContext(ctx, p, "--file-selection", "--multiple", "--separator=\n"), nil
	}
	if p, err := exec.LookPath("kdialog"); err == nil {
		return exec.CommandContext(ctx, p, "--getopenfilename", "--multiple", "--separate-output"), nil
	}
	return nil, errors.New("no file dialog found (needs osascript, zenity or kdialog); type @ to pick a path instead")
}

// pickAttachments opens the OS file dialog for the form's current field.
func (m *Model) pickAttachments() tea.Cmd {
	f := m.form
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		cmd, err := pickCommand(ctx)
		if err != nil {
			return attachPickedMsg{form: f, err: err}
		}
		out, err := cmd.Output()
		if err != nil {
			// A cancelled dialog exits non-zero with nothing printed.
			if _, ok := err.(*exec.ExitError); ok && len(out) == 0 {
				return attachPickedMsg{form: f}
			}
			return attachPickedMsg{form: f, err: err}
		}
		var paths []string
		for _, l := range strings.Split(string(out), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				paths = append(paths, l)
			}
		}
		return attachPickedMsg{form: f, paths: paths}
	}
}

// attach adds `@path` references for paths to the current field: relative to
// the form's cwd when inside it, absolute otherwise, in double quotes when it
// holds whitespace. A path with a double quote or a newline cannot be a
// reference and is reported back.
func (f *form) attach(paths []string) (skipped []string) {
	cur := &f.Fields[f.Cur]
	for _, p := range paths {
		if rel := relativeTo(f.root(), p); rel != "" {
			p = rel
		}
		ref, ok := complete.Quote(p)
		if !ok {
			skipped = append(skipped, p)
			continue
		}
		if cur.Value != "" && !strings.HasSuffix(cur.Value, " ") && !strings.HasSuffix(cur.Value, "\n") {
			cur.Value += " "
		}
		cur.Value += "@" + ref + " "
	}
	cur.at, cur.sl, cur.hl = 0, 0, 0
	return skipped
}

// enableAttach makes field i take @ file references and, for the harness the
// function names, slash commands.
func (f *form) enableAttach(i int, harness func() string) {
	f.Fields[i].Attach = true
	f.Fields[i].Slash = func() []string { return complete.Slash(harness(), f.root()) }
}

// expandHome turns a leading ~ into the home folder.
func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return home + p[1:]
	}
	return p
}

// relativeTo is p relative to root when p is inside it, resolving symlinks on
// both (a macOS /tmp is /private/tmp), and empty otherwise.
func relativeTo(root, p string) string {
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	q, err := filepath.EvalSymlinks(p)
	if err != nil {
		q = p
	}
	rel, err := filepath.Rel(r, q)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	return rel
}
