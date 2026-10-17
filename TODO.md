i have a ton of change i want to make.  i will give you a list.  use subagents to implement them and commit each one separately.

- [x] introduce the concept of a project
- [x] introduce concept of a task and subtask, try to use a single self-referencing table
- [x] store these things in the user's XDG DATA dir
- [x] introduce themes for projects and tasks/subtasks
- [x] tasks are assigned to projects, subtasks inherit project from parent task
- [x] tasks all have their own theme independent of parent/project
- [x] agents/terminals need to be assigned to a task (which can be a subtask) and
inherit the project, optional at creation time...but if no task...choosing
project is required
- [x] i want to introduce a command palette that makes setting options easy with
autocomplete (for tuios verbs)  tuios commands are their own category, then
there are other commands used to group things like assigning selected
terminal/agent to a task or project, jump to tasks page
- [x] there should be an always available tasks page used to easily manage projects
  and tasks and subtasks.
- [x] there should be another page for reviewing all captures in our captures dir.
- [x] i want a keybinding that will cycle how the left rail is grouped.  groups
include:
  - [x] cwd
  - [x] project
  - [x] task
  - [x] top-level task
  - [x] harness
  - [x] ...anything else that can be used to group our items...


---


- [x] for each terminal/agent in the left rail, display line in the format of:
  - first 4 characters of project name, using colors from project's theme
  - followed by task name using colors from the task's theme
- [x] all projects and tasks/subtasks get auto-assigned a random theme from the
  pool of unassigned themes.  a theme is only considered assigned if it has a
  project/task in a non-final state (final states include: done, skipped)
- [x] btw if not setup yet, projects and tasks have a state property
- [x] ideally captures happen automatically on completions of agent turn or
shell command completion
- [x] when prompted for a task title...need a binding walk up the tree for
parent.  meaning...i don't want to have to be on a project row to create a
top-level task within it
- [x] right now `e` renames a task.  it should be edit instead with with
mutliple fields for the various properties: title, parent, status and something
like parent should have an auto-complete fuzzy search for selecting the option.
- [x] grouping a rail should be configurable to have multiple, nested groups.
one, for example, could simply be Project > Parent Task (if exists).  or even
something like Harness > Project > Parent Task
- [x] `r` to refresh causes a panic
- [x] i don't like filling in all info in the bottom prompt...would rather a
modal-style popover with fuzzy-find autocompletion, etc.  especially for agent
prompts...those should have things like @ autocomplete for file references, etc.
- [x] alternate view for terminals/agents...
  - [x] update bindingsn so h/l move in/out of rail and main content area
  - alternate view is like accordian for all prompts in agent session
    and commands submitting in terminal, and maybe both if pane has both
  commands and agent session in scrollback.
  - should also have a filterable view that will extract all links, references
  to files/dirs, PRs, etc. in scrollback or agent responses.  links and prompts
  can be assigned to a project and/or task, and starred, and pinned.  these are
  context items...should be able to pull up a filterable list of context items
  for anythingin the rails (project, task, individual session, etc.)  and they
  should have actions like copy/insert/open/open with..., etc.  they should
  probably have their own page like tasks, and be sortable, filterable,
  groupable, etc.  one reason is it will probbaly be common to sort by created
  at to see most recent items.
- [x] in text fields or prompts accepting input, and have autocomplete lists,
ctrl+n/ctrl+p can be used to go down/up in the list respectively
- [x] any terminal or agent not assigned to a project/task should be displayed
in the left rail with an obnoxious red/white theme.
- [x] whenever asked to choose a project or task, there should always be an
option to add a new one...and it should be a multi-line screen/input that makes
it extremely easy to add multiple projects/tasks/subtasks both brand new or add
sub-items to existing items.  then once they are created, the user can choose
the one for assignment.  also, this mass-input screen should always be
available.
- [x] delete confirmation prompt default to Y or yes instead of N or no.
- [x] once command palette is open....esc closes it...not ctrl-p so ctrl-p can
be used to walk up in the list of options

---

- [ ] need a way to jump in list by group or nesting level probably `{` and `}`


