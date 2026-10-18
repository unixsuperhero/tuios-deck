package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/tuios-deck/internal/review"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

type reviewPage struct {
	rows     []store.Review
	selected string
	opened   *store.Review
	history  bool
	scroll   int
	busy     bool
	err      string
}

type reviewsMsg struct{ err error }

func (m *Model) openReviews() tea.Cmd {
	m.page = pageReviews
	m.rebuildReviews()
	return m.refreshReviews()
}

func (m *Model) refreshReviews() tea.Cmd {
	if m.reviews.busy {
		return nil
	}
	m.reviews.busy = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return reviewsMsg{review.Collect(ctx, m.client, m.store)}
	}
}

func (m *Model) rebuildReviews() {
	p := &m.reviews
	rows := m.store.Reviews()
	p.rows = nil
	for _, r := range rows {
		if (r.ReviewedAt != 0) == p.history {
			p.rows = append(p.rows, r)
		}
	}
	for _, r := range p.rows {
		if r.ID == p.selected {
			return
		}
	}
	p.selected = ""
	if len(p.rows) > 0 {
		p.selected = p.rows[0].ID
	}
}

func (m *Model) reviewIndex() int {
	for i, r := range m.reviews.rows {
		if r.ID == m.reviews.selected {
			return i
		}
	}
	return -1
}

func (m *Model) handleReviewsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := &m.reviews
	switch msg.String() {
	case "esc":
		m.page = pageDeck
		return m, m.loadDetail(true)
	case "r":
		return m, m.refreshReviews()
	case "tab", "shift+tab":
		p.history = !p.history
		p.selected = ""
		m.rebuildReviews()
	case "j", "down", "k", "up":
		i := m.reviewIndex()
		if msg.String() == "j" || msg.String() == "down" {
			i++
		} else {
			i--
		}
		if i >= 0 && i < len(p.rows) {
			p.selected = p.rows[i].ID
		}
	case "enter":
		if p.selected == "" {
			return m, nil
		}
		r, err := m.store.OpenReview(p.selected)
		if err != nil {
			m.setStatus("error: " + err.Error())
			return m, nil
		}
		if p.opened == nil || p.opened.ID != r.ID {
			p.scroll = 0
		}
		p.opened = &r
		m.rebuildReviews()
	case "J", "pgdown", "ctrl+d":
		p.scroll += max(1, m.detailHeight()/2)
	case "K", "pgup", "ctrl+u":
		p.scroll = max(0, p.scroll-max(1, m.detailHeight()/2))
	}
	return m, nil
}

func (m *Model) reviewListLines(width, height int) []string {
	p := &m.reviews
	lines := make([]string, max(0, height))
	if height <= 0 {
		return lines
	}
	title := "Review queue"
	if p.history {
		title = "Reviewed history"
	}
	lines[0] = cHeader.Render(fmt.Sprintf(" %s (%d)", title, len(p.rows)))
	if len(p.rows) == 0 && height > 1 {
		lines[1] = cDim.Render(" no completed prompts")
	}
	start := max(0, m.reviewIndex()-height+2)
	for i, row := range p.rows[start:] {
		if i+1 >= height {
			break
		}
		line := " " + captureTime(time.Unix(0, row.CompletedAt)) + " " + panesReviewTitle(row)
		line = ansi.Truncate(line, width, "…")
		if row.ID == p.selected {
			line = cSel.Render(pad(line, width))
		}
		lines[i+1] = line
	}
	return lines
}

func panesReviewTitle(r store.Review) string {
	return strings.Join(strings.Fields(r.Prompt), " ")
}

func (m *Model) reviewDetailLines(width, height int) []string {
	p := &m.reviews
	lines := make([]string, max(0, height))
	if height <= 0 {
		return lines
	}
	if p.opened == nil {
		lines[0] = cDim.Render(" Enter opens a prompt and marks it reviewed.")
		if p.err != "" && height > 1 {
			lines[1] = cErr.Render(ansi.Truncate(p.err, width, "…"))
		}
		return lines
	}
	r := p.opened
	response := r.Response
	if response == "" {
		response = "No response text was reported by this harness."
	}
	text := fmt.Sprintf("%s › %s · %s\nCompleted %s\nReviewed %s\nSource: agent activity (may be truncated by the harness)\n\nPROMPT\n%s\n\nRESPONSE\n%s", r.Session, r.Pane, r.Harness, time.Unix(0, r.CompletedAt).Format(time.RFC3339), time.UnixMilli(r.ReviewedAt).Format(time.RFC3339), r.Prompt, response)
	if r.Output != "" {
		text += "\n\nPANE OUTPUT AT COLLECTION (may include earlier turns)\n" + r.Output
	}
	body := strings.Split(ansi.Hardwrap(ansi.Strip(text), max(1, width), false), "\n")
	p.scroll = min(max(0, p.scroll), max(0, len(body)-height))
	copy(lines, body[p.scroll:min(len(body), p.scroll+height)])
	return lines
}
