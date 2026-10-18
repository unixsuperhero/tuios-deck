package store

import "strings"

// GlyphSection is a labelled group of the glyphs the glyph tables offer.
type GlyphSection struct {
	Label  string   `json:"label"`
	Glyphs []string `json:"glyphs"`
}

// The glyphs are single graphemes that draw one or two columns wide in
// common terminals and fonts.
var glyphSections = []GlyphSection{
	{"shapes", strings.Fields("◆ ◇ ◈ ● ○ ◉ ◎ ◌ ◐ ◑ ◒ ◓ ■ □ ▪ ▫ ▣ ▤ ▥ ▦ ▲ △ ▼ ▽ ▶ ▷ ◀ ◁ ★ ☆ ⬢ ⬡ ⬟ ♦ ♠ ♣ ♥ ♢ ♤ ♧ ♡ ░ ▒ ▓ █ ▌ ▐ ┼ ╬ ═")},
	{"arrows", strings.Fields("← → ↑ ↓ ↔ ↕ ↖ ↗ ↘ ↙ ⇐ ⇒ ⇑ ⇓ ⇔ ↺ ↻ ↩ ↪ ➜ ➤ ⟶ ⤴ ⤵")},
	{"marks", strings.Fields("✓ ✔ ✗ ✘ ✦ ✧ ✱ ✳ ✴ ✶ ✸ ✹ ❖ ❋ ✿ ❀ ⚑ ⚐ ⚙ ⚡ ☰ ☯ ☀ ☁ ☂ ♪ ♫ ⌂ ⌘ ⌥ ∞ § ¶ † ‡ ※ ⊕ ⊗ ⊘ ⊙ ∴ ≡ ± λ π Σ Ω µ")},
	{"letters", strings.Fields("A B C D E F G H I J K L M N O P Q R S T U V W X Y Z")},
	{"digits", strings.Fields("0 1 2 3 4 5 6 7 8 9")},
}

// GlyphSections is the glyph table, section by section, in order.
func GlyphSections() []GlyphSection {
	out := make([]GlyphSection, len(glyphSections))
	for i, s := range glyphSections {
		out[i] = GlyphSection{s.Label, append([]string{}, s.Glyphs...)}
	}
	return out
}
