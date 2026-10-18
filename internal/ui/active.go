package ui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/gitrepo"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// The active project and task are the one pair the deck works in: the
// title names them, the new agent, terminal and editor forms default to
// them, and a new pane starts in the project's folder.

// openSwitchActive chooses the active project or task: on the tasks page
// the selected row becomes active at once; anywhere else the tree picker
// opens on the current pair, with "+ new project…" at its end.
func (m *Model) openSwitchActive() tea.Cmd {
	if m.page == pageTasks {
		if r := m.selectedRow(); r != nil {
			if r.Task != nil {
				return m.setActive("", r.Task.ID)
			}
			return m.setActive(r.Project.ID, "")
		}
	}
	a := m.store.Active()
	return m.openTreeView(protoRun{purpose: purposeActive, title: "active project or task"}, a.ProjectID, a.TaskID)
}

// setActive makes the project, or the task and its project, active; both
// empty clears the pair.
func (m *Model) setActive(projectID, taskID string) tea.Cmd {
	if err := m.store.SetActive(projectID, taskID); err != nil {
		m.setStatus("error: " + err.Error())
		return nil
	}
	if name := m.activeName(); name != "" {
		m.setStatus("active: " + name)
	} else {
		m.setStatus("no active project; new panes start where the selected pane is")
	}
	m.rebuildTasks()
	return nil
}

// activeName is the active pair as "project › task", "" when none is set.
func (m *Model) activeName() string {
	a := m.store.Active()
	p := m.store.Project(a.ProjectID)
	if p == nil {
		return ""
	}
	s := p.Name
	if t := m.store.Task(a.TaskID); t != nil {
		s += " › " + t.Title
	}
	return s
}

// activeTitle is the title line's active segment: ▶, the project in its
// colour and the task in its own; "" when none is set.
func (m *Model) activeTitle() string {
	a := m.store.Active()
	p := m.store.Project(a.ProjectID)
	if p == nil {
		return ""
	}
	s := cDim.Render("▶ ") + themeStyle(p.Theme).Render(p.Theme.Glyph+" "+p.Name)
	if t := m.store.Task(a.TaskID); t != nil {
		s += cDim.Render(" › ") + themeStyle(t.Theme).Render(t.Title)
	}
	return s
}

// isActive says whether a project or a task row is the active one.
func (m *Model) isActive(projectID, taskID string) bool {
	a := m.store.Active()
	if taskID != "" {
		return a.TaskID == taskID
	}
	return a.TaskID == "" && projectID != "" && a.ProjectID == projectID
}

// targetDir is the folder of the project that a form's task or project
// text names, "" when neither resolves or the project has no repository.
func (m *Model) targetDir(task, project string) string {
	taskID, projectID, _, msg := m.resolveTarget(task, project, 0, 0, true)
	if msg != "" {
		return ""
	}
	if taskID != "" {
		projectID = m.store.ProjectOf(taskID)
	}
	if p := m.store.Project(projectID); p != nil {
		return p.Dir()
	}
	return ""
}

// startDir is where a new pane starts: the cwd typed, else the preset's,
// else the folder of the project the form's task or project names, else
// the selected pane's.
func (m *Model) startDir(typed string, pr *store.Preset, task, project string, p *panes.Pane) string {
	switch {
	case strings.TrimSpace(typed) != "":
		return typed
	case pr != nil && pr.Cwd != "":
		return pr.Cwd
	}
	if d := m.targetDir(task, project); d != "" {
		return d
	}
	return cwdDefault(p)
}

const (
	repoHint     = "the folder of the git repository the project is based on; a folder that is not one is made one with git init · ctrl+o chooses a folder"
	worktreeHint = "a worktree of that repository to work in; empty is the repository itself"
)

// worktreeOptions lists the worktrees of the repository the form's repo
// field names, asking git once per value of that field.
func (m *Model) worktreeOptions(f *form, repoIdx int) func() []string {
	var last string
	var cached []string
	return func() []string {
		repo := expandHome(f.Fields[repoIdx].result())
		if repo == last {
			return cached
		}
		last, cached = repo, nil
		if repo == "" {
			return nil
		}
		root, err := gitrepo.Root(repo)
		if err != nil {
			return nil
		}
		wts, err := gitrepo.Worktrees(root)
		if err != nil {
			return nil
		}
		for _, w := range wts {
			cached = append(cached, w.Path)
		}
		return cached
	}
}

// checkRepoFields refuses a worktree without a repo, or one that is not a
// folder; repoIdx is the repo field, the worktree field follows it.
func checkRepoFields(repo, worktree string, repoIdx int) (int, string) {
	repo, worktree = expandHome(repo), expandHome(worktree)
	if repo == "" && worktree != "" {
		return repoIdx, "a worktree needs its repo"
	}
	if repo != "" {
		if st, err := os.Stat(repo); err == nil && !st.IsDir() {
			return repoIdx, repo + " is not a folder"
		}
	}
	if worktree != "" {
		if st, err := os.Stat(worktree); err != nil || !st.IsDir() {
			return repoIdx + 1, "no folder " + worktree
		}
	}
	return 0, ""
}

// prepareRepo makes the repo folder a repository when it is not one yet
// (creating the folder when it is missing) and gives back its root and the
// worktree, both as typed with ~ expanded; an empty repo gives nothing.
func prepareRepo(repo, worktree string) (root, wt string, err error) {
	repo, worktree = expandHome(repo), expandHome(worktree)
	if repo == "" {
		return "", "", nil
	}
	if err := gitrepo.Init(repo); err != nil {
		return "", "", err
	}
	root, err = gitrepo.Root(repo)
	if err != nil {
		return "", "", err
	}
	return root, worktree, nil
}
