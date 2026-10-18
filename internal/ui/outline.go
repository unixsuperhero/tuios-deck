package ui

import (
	"strings"
	"unicode/utf8"
)

// The editing of an Outline field: unlike other Multi fields, which type
// at the end, it keeps a cursor, pos, a byte offset into the value.

// cursor is pos kept inside the value, which can change under it.
func (fl *field) cursor() int {
	if fl.pos < 0 {
		fl.pos = 0
	}
	if fl.pos > len(fl.Value) {
		fl.pos = len(fl.Value)
	}
	return fl.pos
}

// lineAt is the start and end of the line holding offset p.
func lineAt(v string, p int) (start, end int) {
	start = strings.LastIndex(v[:p], "\n") + 1
	end = len(v)
	if i := strings.Index(v[p:], "\n"); i >= 0 {
		end = p + i
	}
	return start, end
}

// cursorLine is the cursor's line number from 0 and its column in runes.
func (fl *field) cursorLine() (line, col int) {
	p := fl.cursor()
	start, _ := lineAt(fl.Value, p)
	return strings.Count(fl.Value[:p], "\n"), utf8.RuneCountInString(fl.Value[start:p])
}

// insert types s at the cursor.
func (fl *field) insert(s string) {
	p := fl.cursor()
	fl.Value = fl.Value[:p] + s + fl.Value[p:]
	fl.pos = p + len(s)
}

// outlineKey edits an Outline field for the keys that move or edit at the
// cursor, and reports whether it took the key.
func (fl *field) outlineKey(key string) bool {
	v, p := fl.Value, fl.cursor()
	start, end := lineAt(v, p)
	switch key {
	case "up", "ctrl+p", "down", "ctrl+n":
		_, col := fl.cursorLine()
		var s, e int
		if key == "up" || key == "ctrl+p" {
			if start == 0 {
				return true
			}
			s, e = lineAt(v, start-1)
		} else {
			if end == len(v) {
				return true
			}
			s, e = lineAt(v, end+1)
		}
		fl.pos = s + len(firstRunes(v[s:e], col))
	case "left":
		if p > 0 {
			_, n := utf8.DecodeLastRuneInString(v[:p])
			fl.pos = p - n
		}
	case "right":
		if p < len(v) {
			_, n := utf8.DecodeRuneInString(v[p:])
			fl.pos = p + n
		}
	case "home", "ctrl+a":
		fl.pos = start
	case "end", "ctrl+e":
		fl.pos = end
	case "enter":
		// The new line keeps the indent of the one it was split from.
		line := v[start:end]
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		rest := strings.TrimLeft(v[p:end], " \t")
		fl.Value = v[:p] + "\n" + indent + rest + v[end:]
		fl.pos = p + 1 + len(indent)
	case "backspace":
		if p > 0 {
			_, n := utf8.DecodeLastRuneInString(v[:p])
			fl.Value, fl.pos = v[:p-n]+v[p:], p-n
		}
	case "tab", "shift+tab":
		line := indentLast(v[start:end], key == "tab")
		fl.Value = v[:start] + line + v[end:]
		fl.pos = p + len(line) - (end - start)
		if fl.pos < start {
			fl.pos = start
		}
	case "ctrl+u":
		fl.Value, fl.pos = v[:start]+v[end:], start
	case "ctrl+w":
		left := strings.TrimRight(v[start:p], " ")
		if i := strings.LastIndex(left, " "); i >= 0 {
			left = left[:i+1]
		} else {
			left = ""
		}
		fl.Value, fl.pos = v[:start]+left+v[p:], start+len(left)
	default:
		return false
	}
	return true
}

// firstRunes is the first n runes of s, or all of it.
func firstRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}
