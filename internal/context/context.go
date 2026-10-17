// Package context finds context items in what a pane holds: links, pull
// requests, files and folders in its scrollback, the prompts the person
// gave its agent and the commands its shell ran. Gather reads those from
// the daemon; Extract does the finding on one piece of text.
package context

import (
	stdctx "context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// Item is a context item as the store keeps it.
type Item = store.ContextItem

// Source is where a piece of text came from. As says what the text is:
// "" for screen text that is searched for links and paths, "prompt" for a
// prompt the person gave and "command" for a command line; a prompt or a
// command is itself an item, and is searched too.
type Source struct {
	Session string
	Window  string
	Pane    string
	Cwd     string
	As      string
}

// MaxText is the longest an item's text may be.
const MaxText = 500

var (
	urlRe     = regexp.MustCompile("https?://[^\\s<>\"'`]+")
	prURLRe   = regexp.MustCompile(`^https?://github\.com/([\w.-]+/[\w.-]+)/(?:pull|issues)/(\d+)`)
	repoRefRe = regexp.MustCompile(`(?:^|[\s(\[])([A-Za-z0-9][\w.-]*/[\w.-]+)#(\d+)\b`)
	hashRefRe = regexp.MustCompile(`(?:^|[\s(\[])#(\d+)\b`)
	pathRe    = regexp.MustCompile("(?:^|[\\s'\"(\\[=])((?:~|\\.{1,2})?/[^\\s'\"()<>\\[\\]:,;`]+|[\\w.-]+/[^\\s'\"()<>\\[\\]:,;`]+)")
)

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= MaxText {
		return s
	}
	cut := MaxText
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// trimURL drops the punctuation that ends a sentence or closes a bracket
// after a link.
func trimURL(u string) string {
	u = strings.TrimRight(u, ".,;:!?")
	for strings.HasSuffix(u, ")") && strings.Count(u, "(") < strings.Count(u, ")") {
		u = strings.TrimSuffix(u, ")")
	}
	return strings.TrimRight(u, ".,;:!?]}")
}

// Extract finds the items in text. Links are http and https URLs; a GitHub
// pull request or issue URL is a pr instead, as is owner/repo#123, and a
// bare #123 when the text also holds a pull request URL, which names its
// repository. Paths are absolute, ~/ or, with a cwd in the source,
// relative ones, kept only when they exist here: a folder is a dir, else a
// file, by their absolute path. Every text is cut at MaxText.
func Extract(text string, src Source) []Item {
	var out []Item
	add := func(kind, t string) {
		t = clip(t)
		if t == "" {
			return
		}
		for _, o := range out {
			if o.Kind == kind && o.Text == t {
				return
			}
		}
		out = append(out, Item{Kind: kind, Text: t, Session: src.Session, Window: src.Window, Pane: src.Pane})
	}
	switch src.As {
	case "prompt", "command":
		add(src.As, text)
	}

	repo := ""
	rest := text
	for _, loc := range urlRe.FindAllStringIndex(text, -1) {
		u := trimURL(text[loc[0]:loc[1]])
		if m := prURLRe.FindStringSubmatch(u); m != nil {
			add("pr", u)
			if repo == "" {
				repo = m[1]
			}
		} else {
			add("link", u)
		}
	}
	// Links are blanked out so their paths are not taken for files.
	rest = urlRe.ReplaceAllStringFunc(rest, func(s string) string { return strings.Repeat(" ", len(s)) })
	for _, m := range repoRefRe.FindAllStringSubmatch(rest, -1) {
		add("pr", m[1]+"#"+m[2])
	}
	if repo != "" {
		for _, m := range hashRefRe.FindAllStringSubmatch(rest, -1) {
			add("pr", repo+"#"+m[1])
		}
	}
	for _, m := range pathRe.FindAllStringSubmatch(rest, -1) {
		if kind, abs := statPath(trimURL(m[1]), src.Cwd); kind != "" {
			add(kind, abs)
		}
	}
	return out
}

// statPath resolves a path as written and reports whether it is a file or
// a folder on this machine, with its absolute form. A relative path needs
// the cwd it is relative to.
func statPath(p, cwd string) (kind, abs string) {
	if len(p) < 2 {
		return "", ""
	}
	switch {
	case strings.HasPrefix(p, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", ""
		}
		abs = filepath.Join(home, p[2:])
	case filepath.IsAbs(p):
		abs = filepath.Clean(p)
	default:
		if cwd == "" {
			return "", ""
		}
		abs = filepath.Join(cwd, p)
	}
	if abs == "/" {
		return "", ""
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", ""
	}
	if fi.IsDir() {
		return "dir", abs
	}
	return "file", abs
}

// Unwrap joins the rows a terminal wrapped, so a long link or path is
// whole again. A capture does not mark its wrapped rows, so a row as wide
// as the widest one in the capture, which is the pane's width when
// anything wrapped, is taken to continue on the next row.
func Unwrap(text string) string {
	lines := strings.Split(text, "\n")
	width := 0
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
		if n := utf8.RuneCountInString(lines[i]); n > width {
			width = n
		}
	}
	if width < 20 {
		return strings.Join(lines, "\n")
	}
	var b strings.Builder
	for i, l := range lines {
		b.WriteString(l)
		if i < len(lines)-1 && utf8.RuneCountInString(l) < width {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Counts is how much a Gather read, by where it came from.
type Counts struct {
	Screen     int    `json:"screen"`
	Activity   int    `json:"activity"`
	Transcript int    `json:"transcript"`
	History    int    `json:"history"`
	Skipped    string `json:"skipped,omitempty"` // what could not be read, and why
}

// Gather reads a pane's scrollback, its agent's activity, its transcript
// when a nonce is given, and its shell's command history, and extracts
// the items of each. Only the scrollback must be readable; the others are
// skipped, and said so in Counts, when the pane has none.
func Gather(ctx stdctx.Context, c *daemon.Client, src Source, nonce string) ([]Item, Counts, error) {
	var items []Item
	var counts Counts
	var skipped []string
	cap, err := c.CapturePane(ctx, src.Session, src.Window, 2000, false)
	if err != nil {
		return nil, counts, err
	}
	found := Extract(Unwrap(cap.Content), src)
	counts.Screen = len(found)
	items = append(items, found...)

	if a, err := c.Activity(ctx, src.Session, src.Window, 256); err == nil {
		for _, e := range a.Entries {
			s := src
			var found []Item
			switch e.Kind {
			case "prompt":
				s.As = "prompt"
				found = Extract(e.Text, s)
			default:
				found = Extract(strings.Join(append([]string{e.Target, e.Text}, e.Files...), "\n"), s)
			}
			counts.Activity += len(found)
			items = append(items, found...)
		}
	} else {
		skipped = append(skipped, "activity: "+err.Error())
	}

	if nonce != "" {
		t, err := c.Transcript(ctx, src.Session, src.Window, nonce, 400)
		var derr *daemon.Error
		switch {
		case err == nil:
			for _, e := range t.Entries {
				if e.Kind == "thinking" {
					continue
				}
				s := src
				if e.Role == "user" && (e.Kind == "prompt" || e.Kind == "text" || e.Kind == "message" || e.Kind == "") {
					s.As = "prompt"
				}
				found := Extract(e.Text+"\n"+e.Target, s)
				counts.Transcript += len(found)
				items = append(items, found...)
			}
		case errors.As(err, &derr) && derr.Code == "no_transcript":
		default:
			skipped = append(skipped, "transcript: "+err.Error())
		}
	}

	if hist, err := c.CommandHistory(ctx, src.Session, src.Window, 200); err == nil {
		for _, h := range hist {
			s := src
			s.As = "command"
			if h.Cwd != "" {
				s.Cwd = h.Cwd
			}
			found := Extract(h.Cmdline, s)
			counts.History += len(found)
			items = append(items, found...)
		}
	} else {
		skipped = append(skipped, "command-history: "+err.Error())
	}
	counts.Skipped = strings.Join(skipped, "; ")
	return items, counts, nil
}
