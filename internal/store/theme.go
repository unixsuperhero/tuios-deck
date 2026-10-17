package store

import "strconv"

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

// NextTheme is the theme a new project or task gets when none is given: the
// next colour round-robin over the palette, skipping the two blacks, with
// the task glyph. Which colour comes next follows from how many projects
// and tasks the store holds, so it needs no counter of its own.
func (s *Store) NextTheme() Theme {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextTheme()
}

func (s *Store) nextTheme() Theme {
	usable := palette[1:8]
	usable = append(append([]string{}, usable...), palette[9:]...)
	n := len(s.projects) + len(s.tasks)
	return Theme{Color: usable[n%len(usable)], Glyph: "●"}
}
