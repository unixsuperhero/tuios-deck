i have a ton of change i want to make.  i will give you a list.  use subagents to implement them and commit each one separately.

- introduce the concept of a project
- introduce concept of a task and subtask, try to use a single self-referencing table
- store these things in the user's XDG DATA dir
- introduce themes for projects and tasks/subtasks
- tasks are assigned to projects, subtasks inherit project from parent task
- tasks all have their own theme independent of parent/project
- agents/terminals need to be assigned to a task (which can be a subtask) and
inherit the project, optional at creation time...but if no task...choosing
project is required
- i want to introduce a command palette that makes setting options easy with
autocomplete (for tuios verbs)  tuios commands are their own category, then
there are other commands used to group things like assigning selected
terminal/agent to a task or project, jump to tasks page
- there should be an always available tasks page used to easily manage projects
  and tasks and subtasks.
- there should be another page for reviewing all captures in our captures dir.
- i want a keybinding that will cycle how the left rail is grouped.  groups
include:
  - cwd
  - project
  - task
  - top-level task
  - harness
  - ...anything else that can be used to group our items...

