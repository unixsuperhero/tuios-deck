package ui

// The group keys: { and } jump to the previous or next group at the same
// nesting depth, ( and ) step one nesting level up or down. They stop at
// the ends and say nothing there.

// seek is the first index from start in direction dir, within n, where ok
// holds, or -1.
func seek(n, start, dir int, ok func(int) bool) int {
	for i := start; i >= 0 && i < n; i += dir {
		if ok(i) {
			return i
		}
	}
	return -1
}

// railDepths is how many header depths the rail has: one per level of the
// grouping, and one for the plain session grouping.
func (m *Model) railDepths() int {
	return max(1, len(m.grouping))
}

// railLevel is the header depth { and } jump between: the innermost one,
// less the levels ( has stepped up.
func (m *Model) railLevel() int {
	return max(0, m.railDepths()-1-m.railUp)
}

// railHeader is the header at depth d that holds row i, or -1.
func (m *Model) railHeader(i, d int) int {
	return seek(len(m.rows), i, -1, func(j int) bool { return m.rows[j].Pane == nil && m.rows[j].Depth == d })
}

// railSelect selects the first pane under the header at h, the way j and k
// select: the detail re-reads from its end.
func (m *Model) railSelect(h int) {
	if h < 0 {
		return
	}
	if p := m.firstPane(h+1, 1); p >= 0 && p != m.sel {
		m.sel = p
		m.detail, m.detailErr, m.revision = nil, "", 0
		m.follow = true
	}
}

// railJump moves to the first pane of the previous or next group at the
// rail's level.
func (m *Model) railJump(dir int) {
	d := m.railLevel()
	h := m.railHeader(m.sel, d)
	if h < 0 {
		return
	}
	m.railSelect(seek(len(m.rows), h+dir, dir, func(j int) bool { return m.rows[j].Pane == nil && m.rows[j].Depth == d }))
}

// railNest steps the rail's level up (-1) to the group holding the current
// one, selecting its first pane, or down (+1) to the first group inside the
// current one. It says the level it reached.
func (m *Model) railNest(dir int) {
	d := m.railLevel()
	if dir < 0 && d == 0 || dir > 0 && m.railUp == 0 {
		return
	}
	m.railUp -= dir
	m.railSelect(m.railHeader(m.sel, min(d, m.railLevel())))
	name := lvSession.String()
	if len(m.grouping) > 0 {
		name = m.grouping[m.railLevel()].String()
	}
	m.setStatus("{/} jump by " + name)
}

// taskJump moves the tasks page's selection to the previous or next row at
// the same depth: the next project from a project, the next task at that
// depth from a task.
func (m *Model) taskJump(dir int) {
	if m.tsel < 0 || m.tsel >= len(m.trows) {
		return
	}
	d := m.trows[m.tsel].Depth
	if i := seek(len(m.trows), m.tsel+dir, dir, func(j int) bool { return m.trows[j].Depth == d }); i >= 0 {
		m.tselect(i)
	}
}

// taskNest moves the tasks page's selection to its parent row (-1), the
// project or task above it one level up, or to its first child (+1).
func (m *Model) taskNest(dir int) {
	if m.tsel < 0 || m.tsel >= len(m.trows) {
		return
	}
	d := m.trows[m.tsel].Depth
	if dir > 0 {
		if n := m.tsel + 1; n < len(m.trows) && m.trows[n].Depth == d+1 {
			m.tselect(n)
		}
		return
	}
	if d == 0 {
		return
	}
	if i := seek(len(m.trows), m.tsel-1, -1, func(j int) bool { return m.trows[j].Depth == d-1 }); i >= 0 {
		m.tselect(i)
	}
}

// ctxJump moves the context page's selection to the first item of the
// previous or next group. Ungrouped, it does nothing.
func (m *Model) ctxJump(dir int) {
	n := len(m.cx.rows)
	isHeader := func(j int) bool { return m.cx.rows[j].Item == nil }
	h := seek(n, m.cx.sel, -1, isHeader)
	if h < 0 {
		return
	}
	if g := seek(n, h+dir, dir, isHeader); g >= 0 {
		if i := m.ctxNext(g+1, 1); i >= 0 {
			m.ctxSelect(i)
		}
	}
}

// turnJump moves the Turns tab's selection to the previous or next prompt
// row, past the commands between them.
func (m *Model) turnJump(dir int) {
	i := seek(len(m.turns), m.turnSel+dir, dir, func(j int) bool { return !m.turns[j].Cmd })
	if i < 0 {
		return
	}
	m.turnSel = i
	m.follow = false
	m.renderTurns()
	m.showTurn()
}
