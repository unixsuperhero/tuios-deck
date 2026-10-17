package context

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"syscall"
)

// ErrNoTool is Copy's answer when no clipboard tool is on PATH.
var ErrNoTool = errors.New("no clipboard tool on PATH")

// Copy puts text on this machine's clipboard with the first tool found:
// pbcopy, wl-copy, then xclip. It names the tool it used.
func Copy(text string) (string, error) {
	for _, tool := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}} {
		if _, err := exec.LookPath(tool[0]); err != nil {
			continue
		}
		cmd := exec.Command(tool[0], tool[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			return tool[0], err
		}
		return tool[0], nil
	}
	return "", ErrNoTool
}

var repoRefFull = regexp.MustCompile(`^([\w.-]+/[\w.-]+)#(\d+)$`)

// Target is what opening an item opens: a link or a pull request URL, a
// short owner/repo#123 as its GitHub URL, or a file or folder path. A
// prompt or a command has none.
func Target(c Item) (string, bool) {
	switch c.Kind {
	case "link", "file", "dir":
		return c.Text, true
	case "pr":
		if m := repoRefFull.FindStringSubmatch(c.Text); m != nil {
			return "https://github.com/" + m[1] + "/issues/" + m[2], true
		}
		return c.Text, true
	}
	return "", false
}

// Opener is the platform's program that opens a URL or a path the way a
// double click would.
func Opener() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}

// Start runs a program on the target detached from the deck: its own
// process group, no terminal, and its exit reaped in the background.
func Start(prog, target string) error {
	args := append(strings.Fields(prog), target)
	cmd := exec.Command(args[0], args[1:]...)
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, devnull, devnull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		devnull.Close()
		return err
	}
	go func() { cmd.Wait(); devnull.Close() }()
	return nil
}

// Programs are the common programs to open an item with that are on PATH,
// the platform's opener first.
func Programs() []string {
	var out []string
	for _, p := range []string{Opener(), "code", "cursor", "nvim", "vim", "less"} {
		if _, err := exec.LookPath(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// Terminal says whether a program needs a terminal to run in.
func Terminal(prog string) bool {
	name := strings.Fields(prog)
	if len(name) == 0 {
		return false
	}
	switch name[0] {
	case "vim", "nvim", "vi", "less", "more", "nano", "emacs", "hx", "micro", "bat":
		return true
	}
	return false
}

// ShellQuote quotes s for a POSIX shell.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
