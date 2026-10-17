package store

import (
	"math/rand/v2"
	"strconv"
)

// Theme is the colour and glyph a project or task is drawn with. Color is
// one of the 16 ANSI colour names from Palette; Glyph is one grapheme.
type Theme struct {
	Color string `json:"color"`
	Glyph string `json:"glyph"`
}

var palette = []string{
	"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
	"bright-black", "bright-red", "bright-green", "bright-yellow",
	"bright-blue", "bright-magenta", "bright-cyan", "bright-white",
}

// The xterm defaults for the 16 colours, in palette order.
var hexes = []string{
	"#000000", "#cd0000", "#00cd00", "#cdcd00", "#0000ee", "#cd00cd", "#00cdcd", "#e5e5e5",
	"#7f7f7f", "#ff0000", "#00ff00", "#ffff00", "#5c5cff", "#ff00ff", "#00ffff", "#ffffff",
}

// Palette is the 16 ANSI colour names in index order.
func Palette() []string {
	out := make([]string, len(palette))
	copy(out, palette)
	return out
}

func colorIndex(name string) int {
	for i, n := range palette {
		if n == name {
			return i
		}
	}
	return -1
}

// ANSI is the lipgloss colour index, "0".."15". An unknown colour is "7".
func (t Theme) ANSI() string {
	i := colorIndex(t.Color)
	if i < 0 {
		i = 7
	}
	return strconv.Itoa(i)
}

// CSS is the colour as a hex string for the web page.
func (t Theme) CSS() string {
	i := colorIndex(t.Color)
	if i < 0 {
		i = 7
	}
	return hexes[i]
}

// usable is the palette without black and bright-black, which are never
// given out on their own.
func usable() []string {
	var out []string
	for _, c := range palette {
		if c != "black" && c != "bright-black" {
			out = append(out, c)
		}
	}
	return out
}

// NextTheme is the theme a new project or task gets when none is given: a
// colour picked at random from the free ones, with the task glyph. When
// every colour is held it is picked at random from all of them but the
// blacks.
func (s *Store) NextTheme() Theme {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextTheme()
}

func (s *Store) nextTheme() Theme {
	pool := s.freeColors(nil)
	if len(pool) == 0 {
		pool = usable()
	}
	return Theme{Color: pool[rand.IntN(len(pool))], Glyph: "●"}
}

// FreeColors is the palette colours, blacks aside, that no open project or
// task holds, in palette order. A done or skipped item holds nothing. The
// items named in except do not count, so a row can ask which colours it
// could move to.
func (s *Store) FreeColors(except ...string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	skip := map[string]bool{}
	for _, id := range except {
		skip[id] = true
	}
	return s.freeColors(skip)
}

func (s *Store) freeColors(except map[string]bool) []string {
	held := map[string]bool{}
	for _, p := range s.projects {
		if !IsFinal(p.Status) && !except[p.ID] {
			held[p.Theme.Color] = true
		}
	}
	for _, t := range s.tasks {
		if !IsFinal(t.Status) && !except[t.ID] {
			held[t.Theme.Color] = true
		}
	}
	var out []string
	for _, c := range usable() {
		if !held[c] {
			out = append(out, c)
		}
	}
	return out
}
