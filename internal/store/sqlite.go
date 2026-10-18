package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// The store lives in one sqlite database. The tables mirror the old JSON
// file's: every row of a table is held in memory too, so reads stay what
// they were, and a mutation writes only the rows it changed, in one
// transaction (see save). History is appended by AddHistory directly.

// schemaVersion 2 added projects.repo and projects.worktree, and the
// active_project and active_task keys of meta.
const schemaVersion = 2

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS projects (id TEXT PRIMARY KEY, name TEXT NOT NULL, theme_color TEXT NOT NULL, theme_glyph TEXT NOT NULL,
	status TEXT NOT NULL, created INTEGER NOT NULL, seq INTEGER NOT NULL, repo TEXT NOT NULL DEFAULT '', worktree TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, parent_id TEXT NOT NULL, project_id TEXT NOT NULL, title TEXT NOT NULL,
	theme_color TEXT NOT NULL, theme_glyph TEXT NOT NULL, status TEXT NOT NULL, easy INTEGER NOT NULL, snoozed_until INTEGER NOT NULL,
	created INTEGER NOT NULL, updated INTEGER NOT NULL, seq INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS tasks_parent ON tasks (parent_id);
CREATE INDEX IF NOT EXISTS tasks_project ON tasks (project_id);
CREATE TABLE IF NOT EXISTS assignments ("window" TEXT PRIMARY KEY, session TEXT NOT NULL, task_id TEXT NOT NULL, project_id TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS context_items (id TEXT PRIMARY KEY, kind TEXT NOT NULL, text TEXT NOT NULL, session TEXT NOT NULL,
	"window" TEXT NOT NULL, pane TEXT NOT NULL, project_id TEXT NOT NULL, task_id TEXT NOT NULL, starred INTEGER NOT NULL,
	pinned INTEGER NOT NULL, created INTEGER NOT NULL, seq INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS context_window ON context_items ("window");
CREATE INDEX IF NOT EXISTS context_task ON context_items (task_id);
CREATE INDEX IF NOT EXISTS context_project ON context_items (project_id);
CREATE TABLE IF NOT EXISTS reminders (id TEXT PRIMARY KEY, title TEXT NOT NULL, message TEXT NOT NULL, interval INTEGER NOT NULL,
	enabled INTEGER NOT NULL, next INTEGER NOT NULL, last INTEGER NOT NULL, created INTEGER NOT NULL, seq INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS notes (id TEXT PRIMARY KEY, text TEXT NOT NULL, task_id TEXT NOT NULL, project_id TEXT NOT NULL,
	created INTEGER NOT NULL, updated INTEGER NOT NULL, seq INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS notes_task ON notes (task_id);
CREATE INDEX IF NOT EXISTS notes_project ON notes (project_id);
CREATE TABLE IF NOT EXISTS prompt_presets (id TEXT PRIMARY KEY, name TEXT NOT NULL, prefix TEXT NOT NULL, cwd TEXT NOT NULL,
	created INTEGER NOT NULL, seq INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS queued_prompts (id TEXT PRIMARY KEY, text TEXT NOT NULL, preset TEXT NOT NULL, task_id TEXT NOT NULL,
	project_id TEXT NOT NULL, cwd TEXT NOT NULL, session TEXT NOT NULL, agent TEXT NOT NULL, "window" TEXT NOT NULL,
	created INTEGER NOT NULL, seq INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS review_marks (id TEXT PRIMARY KEY, reviewed_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS history (id TEXT PRIMARY KEY, kind TEXT NOT NULL, at INTEGER NOT NULL, observed INTEGER NOT NULL,
	source TEXT NOT NULL, source_id TEXT NOT NULL, session TEXT NOT NULL, "window" TEXT NOT NULL, summary TEXT NOT NULL,
	detail TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS history_window_at ON history ("window", at);
CREATE INDEX IF NOT EXISTS history_at ON history (at);
CREATE TABLE IF NOT EXISTS prompt_stashes (stash_key TEXT PRIMARY KEY, draft_id TEXT NOT NULL, text TEXT NOT NULL,
	saved INTEGER NOT NULL, seq INTEGER NOT NULL);
`

// dataTables is every table but meta, for the emptiness check and a
// whole import.
var dataTables = []string{"projects", "tasks", "assignments", "context_items", "reminders", "notes", "prompt_presets", "queued_prompts", "review_marks", "history", "prompt_stashes"}

// DefaultPath is the store: $TUIOS_DECK_DATA, else <DataDir>/deck.db.
func DefaultPath() string {
	if p := os.Getenv("TUIOS_DECK_DATA"); p != "" {
		return p
	}
	return filepath.Join(DataDir(), "deck.db")
}

// Paths is the database and the JSON file to migrate from that a store
// path names. A path ending in .json is the JSON file, and the database is
// beside it with the same name ending in .db (deck.json, deck.db); any
// other path is the database, and its JSON is the same name ending in .json.
func Paths(path string) (db, jsonFile string) {
	if strings.HasSuffix(path, ".json") {
		return strings.TrimSuffix(path, ".json") + ".db", path
	}
	return path, strings.TrimSuffix(path, ".db") + ".json"
}

// Open opens the store a path names (see Paths), creating the database when
// it is missing. A database with no rows takes the JSON file's contents when
// that file exists, and the file is then renamed to <name>.migrated, never
// deleted; one line on stderr says so.
func Open(path string) (*Store, error) {
	dbPath, jsonPath := Paths(path)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}
	// Created here so it is the person's alone; sqlite gives its -wal and
	// -shm files the database's mode.
	if f, err := os.OpenFile(dbPath, os.O_RDWR|os.O_CREATE, 0o600); err != nil {
		return nil, err
	} else {
		f.Close()
	}
	// A write waits this long for another deck's to finish before it fails.
	// TUIOS_DECK_BUSY_TIMEOUT (ms) shortens it for the suites, which hold
	// the lock to make a save fail.
	busy := 10000
	if v, err := strconv.Atoi(os.Getenv("TUIOS_DECK_BUSY_TIMEOUT")); err == nil && v >= 0 {
		busy = v
	}
	db, err := sql.Open("sqlite", fmt.Sprintf("%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath, busy))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{path: dbPath, db: db, tables: newTables()}
	if err := s.init(jsonPath); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", dbPath, err)
	}
	return s, nil
}

func (s *Store) init(jsonPath string) error {
	// The schema is created once; a database that has it is only read
	// here, so a deck export beside a running deck takes no write lock.
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'meta'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := s.db.Exec(schema); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT INTO meta (key, value) VALUES ('schema_version', ?)`, fmt.Sprint(schemaVersion)); err != nil {
			return err
		}
	} else {
		var v string
		if err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v); err != nil {
			return err
		}
		if v == "1" {
			if err := s.migrate1to2(); err != nil {
				return fmt.Errorf("migrating schema 1 to 2: %w", err)
			}
			v = "2"
		}
		if v != fmt.Sprint(schemaVersion) {
			return fmt.Errorf("schema version %s is not the %d this deck knows", v, schemaVersion)
		}
	}
	empty, err := s.empty()
	if err != nil {
		return err
	}
	if empty {
		data, err := os.ReadFile(jsonPath)
		if err == nil {
			if err := s.importJSON(data); err != nil {
				return fmt.Errorf("migrating %s: %w", jsonPath, err)
			}
			if err := os.Rename(jsonPath, jsonPath+".migrated"); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "deck: moved %s into %s; the file is kept as %s.migrated\n", jsonPath, s.path, filepath.Base(jsonPath))
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return s.load()
}

// migrate1to2 gives projects their repo and worktree columns, empty for
// every project that exists.
func (s *Store) migrate1to2() error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, col := range []string{"repo", "worktree"} {
			if _, err := tx.Exec(`ALTER TABLE projects ADD COLUMN ` + col + ` TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`UPDATE meta SET value = '2' WHERE key = 'schema_version'`)
		return err
	})
}

func (s *Store) empty() (bool, error) {
	for _, t := range dataTables {
		var n int
		if err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM ` + t + `)`).Scan(&n); err != nil {
			return false, err
		}
		if n != 0 {
			return false, nil
		}
	}
	return true, nil
}

// load reads every table into memory, then drops what the deck no longer
// keeps: history kinds it stopped recording, history over the cap, and
// marks of turns history no longer holds.
func (s *Store) load() error {
	var err error
	if s.projects, err = s.projectT.load(s.db); err != nil {
		return err
	}
	if s.tasks, err = s.taskT.load(s.db); err != nil {
		return err
	}
	as, err := s.assignT.load(s.db)
	if err != nil {
		return err
	}
	s.assignments = make(map[string]Assignment, len(as))
	for _, a := range as {
		s.assignments[a.Window] = a.Assignment
	}
	if s.contexts, err = s.contextT.load(s.db); err != nil {
		return err
	}
	if s.reminders, err = s.reminderT.load(s.db); err != nil {
		return err
	}
	if s.notes, err = s.noteT.load(s.db); err != nil {
		return err
	}
	if s.presets, err = s.presetT.load(s.db); err != nil {
		return err
	}
	if s.queue, err = s.queueT.load(s.db); err != nil {
		return err
	}
	ms, err := s.markT.load(s.db)
	if err != nil {
		return err
	}
	s.marks = make(map[string]int64, len(ms))
	for _, m := range ms {
		s.marks[m.ID] = m.At
	}
	ps, err := s.stashT.load(s.db)
	if err != nil {
		return err
	}
	s.stashes = make([]StashedPrompt, len(ps))
	for i, p := range ps {
		s.stashes[i] = p.StashedPrompt
	}
	s.active = Active{}
	for key, dst := range map[string]*string{"active_project": &s.active.ProjectID, "active_task": &s.active.TaskID} {
		if err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(dst); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	s.activeSnap = s.active
	rows, err := s.db.Query(`SELECT ` + historyCols + ` FROM history ORDER BY rowid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var h []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		if err := rows.Scan(historyFields(&e)...); err != nil {
			return err
		}
		h = append(h, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	s.history = pruneHistory(h)
	if len(s.history) != len(h) {
		kept := make(map[string]bool, len(s.history))
		for _, e := range s.history {
			kept[e.ID] = true
		}
		var gone []HistoryEntry
		for _, e := range h {
			if !kept[e.ID] {
				gone = append(gone, e)
			}
		}
		if err := s.inTx(func(tx *sql.Tx) error { return deleteHistory(tx, gone) }); err != nil {
			return err
		}
	}
	s.pruneMarks()
	return s.save()
}

// importJSON replaces every table with the old JSON file's contents, read
// as Open read that file before the database: an empty status is open,
// history is pruned, an earlier deck's reviews become marks.
func (s *Store) importJSON(data []byte) error {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	if f.Version > version {
		return fmt.Errorf("version %d is newer than this deck knows", f.Version)
	}
	s.projects = f.Projects
	s.tasks = f.Tasks
	for i := range s.projects {
		if s.projects[i].Status == "" {
			s.projects[i].Status = "open"
		}
	}
	for i := range s.tasks {
		if s.tasks[i].Status == "" {
			s.tasks[i].Status = "open"
		}
	}
	s.assignments = f.Assignments
	if s.assignments == nil {
		s.assignments = map[string]Assignment{}
	}
	s.contexts = f.Contexts
	s.reminders = f.Reminders
	s.notes = f.Notes
	s.presets = f.Presets
	s.queue = f.Queue
	s.history = pruneHistory(f.History)
	s.historyIDs = nil
	s.marks = f.Marks
	if s.marks == nil {
		s.marks = map[string]int64{}
	}
	s.migrateReviews(f.Reviews)
	s.pruneMarks()
	s.active = Active{}
	if f.Active != nil {
		s.active = *f.Active
	}
	// Two stashes with one key cannot both be rows; the first, the newer, stays.
	seen := map[string]bool{}
	s.stashes = nil
	for _, p := range f.Stashes {
		if k := stashKey(p); !seen[k] {
			seen[k] = true
			s.stashes = append(s.stashes, p)
		}
	}
	return s.inTx(func(tx *sql.Tx) error {
		for _, t := range dataTables {
			if _, err := tx.Exec(`DELETE FROM ` + t); err != nil {
				return err
			}
		}
		s.resetSnapshots()
		if err := insertHistory(tx, s.history); err != nil {
			return err
		}
		return s.syncAll(tx)
	})
}

// ImportJSON replaces the whole store with a file in the old JSON shape,
// as WriteJSON writes it.
func (s *Store) ImportJSON(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.importJSON(data); err != nil {
		// What is in memory may be half the file; the database is not.
		s.resetSnapshots()
		return errors.Join(err, s.load())
	}
	return nil
}

// WriteJSON writes the store in the old JSON file's shape, version 1.
func (s *Store) WriteJSON(w io.Writer) error {
	s.mu.Lock()
	f := file{Version: version, Projects: s.projects, Tasks: s.tasks, Assignments: s.assignments, Contexts: s.contexts, Reminders: s.reminders, Notes: s.notes, Presets: s.presets, Queue: s.queue, Marks: s.marks, History: s.history, Stashes: s.stashes}
	if s.active != (Active{}) {
		a := s.active
		f.Active = &a
	}
	if f.Projects == nil {
		f.Projects = []Project{}
	}
	if f.Tasks == nil {
		f.Tasks = []Task{}
	}
	if f.Contexts == nil {
		f.Contexts = []ContextItem{}
	}
	if f.Reminders == nil {
		f.Reminders = []Reminder{}
	}
	if f.Notes == nil {
		f.Notes = []Note{}
	}
	if f.Presets == nil {
		f.Presets = []Preset{}
	}
	if f.Queue == nil {
		f.Queue = []QueuedPrompt{}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// File is the database the store reads and writes.
func (s *Store) File() string { return s.path }

// Save writes whatever in memory differs from the database.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save()
}

// save writes the rows that changed since the last save, every table in one
// transaction. A failed save leaves the database as it was and remembers
// nothing, so the next save writes the same rows again.
func (s *Store) save() error {
	return s.inTx(s.syncAll)
}

func (s *Store) syncAll(tx *sql.Tx) error {
	as := make([]assignRow, 0, len(s.assignments))
	for w, a := range s.assignments {
		as = append(as, assignRow{w, a})
	}
	ms := make([]markRow, 0, len(s.marks))
	for id, at := range s.marks {
		ms = append(ms, markRow{id, at})
	}
	ps := make([]stashedRow, len(s.stashes))
	for i, p := range s.stashes {
		ps[i] = stashedRow{stashKey(p), p}
	}
	commits := make([]func(), 0, 10)
	for _, f := range []func(*sql.Tx) (func(), error){
		func(tx *sql.Tx) (func(), error) { return s.projectT.sync(tx, s.projects) },
		func(tx *sql.Tx) (func(), error) { return s.taskT.sync(tx, s.tasks) },
		func(tx *sql.Tx) (func(), error) { return s.assignT.sync(tx, as) },
		func(tx *sql.Tx) (func(), error) { return s.contextT.sync(tx, s.contexts) },
		func(tx *sql.Tx) (func(), error) { return s.reminderT.sync(tx, s.reminders) },
		func(tx *sql.Tx) (func(), error) { return s.noteT.sync(tx, s.notes) },
		func(tx *sql.Tx) (func(), error) { return s.presetT.sync(tx, s.presets) },
		func(tx *sql.Tx) (func(), error) { return s.queueT.sync(tx, s.queue) },
		func(tx *sql.Tx) (func(), error) { return s.markT.sync(tx, ms) },
		func(tx *sql.Tx) (func(), error) { return s.stashT.sync(tx, ps) },
	} {
		c, err := f(tx)
		if err != nil {
			return err
		}
		if c != nil {
			commits = append(commits, c)
		}
	}
	if s.active != s.activeSnap {
		for key, val := range map[string]string{"active_project": s.active.ProjectID, "active_task": s.active.TaskID} {
			if _, err := tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)`, key, val); err != nil {
				return err
			}
		}
		a := s.active
		commits = append(commits, func() { s.activeSnap = a })
	}
	s.afterCommit = append(s.afterCommit, commits...)
	return nil
}

// inTx runs f in a transaction and, once it commits, the snapshot updates f
// queued in afterCommit.
func (s *Store) inTx(f func(*sql.Tx) error) error {
	s.afterCommit = s.afterCommit[:0]
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		tx.Rollback()
		s.afterCommit = s.afterCommit[:0]
		return err
	}
	if err := tx.Commit(); err != nil {
		s.afterCommit = s.afterCommit[:0]
		return err
	}
	for _, c := range s.afterCommit {
		c()
	}
	s.afterCommit = s.afterCommit[:0]
	return nil
}

func (s *Store) resetSnapshots() {
	s.projectT.snap, s.taskT.snap, s.assignT.snap, s.contextT.snap, s.reminderT.snap = nil, nil, nil, nil, nil
	s.noteT.snap, s.presetT.snap, s.queueT.snap, s.markT.snap, s.stashT.snap = nil, nil, nil, nil, nil
	s.activeSnap = Active{"\x00", "\x00"} // never equal to a real pair, so the next sync writes it
}

// table is one in-memory slice mirrored to one sqlite table. fields gives
// pointers to a row's columns, the key column first; seq, when the table is
// ordered, is an extra column that keeps the slice's order.
type table[T comparable] struct {
	name    string
	cols    []string
	fields  func(*T) []any
	ordered bool
	reverse bool // the slice is newest first: its first row has the highest seq
	snap    map[string]snapRow[T]
}

type snapRow[T comparable] struct {
	v   T
	seq int64
}

func (t *table[T]) colList() string {
	q := make([]string, len(t.cols))
	for i, c := range t.cols {
		q[i] = `"` + c + `"`
	}
	if t.ordered {
		q = append(q, "seq")
	}
	return strings.Join(q, ", ")
}

func (t *table[T]) load(db *sql.DB) ([]T, error) {
	order := ""
	if t.ordered {
		order = " ORDER BY seq"
		if t.reverse {
			order += " DESC"
		}
	}
	rows, err := db.Query(`SELECT ` + t.colList() + ` FROM ` + t.name + order)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	snap := map[string]snapRow[T]{}
	for rows.Next() {
		var v T
		var seq int64
		dest := t.fields(&v)
		if t.ordered {
			dest = append(dest, &seq)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out = append(out, v)
		snap[keyOf(t.fields(&v))] = snapRow[T]{v, seq}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	t.snap = snap
	return out, nil
}

func keyOf(fields []any) string { return *fields[0].(*string) }

// sync writes the rows of cur that are new or changed and deletes the rows
// that are gone. A row keeps its seq while the order allows; one that moved
// before rows of higher seq takes a new one. The returned func, run after
// the commit, makes cur the snapshot; it is nil when nothing changed.
func (t *table[T]) sync(tx *sql.Tx, cur []T) (func(), error) {
	var top int64
	for _, r := range t.snap {
		top = max(top, r.seq)
	}
	next := make(map[string]snapRow[T], len(cur))
	type write struct {
		v   T
		seq int64
	}
	var writes []write
	prev := int64(-1)
	for i := range cur {
		v := cur[i]
		if t.reverse {
			v = cur[len(cur)-1-i]
		}
		k := keyOf(t.fields(&v))
		old, had := t.snap[k]
		seq := old.seq
		if t.ordered && (!had || seq <= prev) {
			top++
			seq = top
		}
		prev = seq
		next[k] = snapRow[T]{v, seq}
		if !had || old.v != v || old.seq != seq {
			writes = append(writes, write{v, seq})
		}
	}
	var gone []string
	for k := range t.snap {
		if _, ok := next[k]; !ok {
			gone = append(gone, k)
		}
	}
	if len(writes) == 0 && len(gone) == 0 {
		return nil, nil
	}
	if len(writes) > 0 {
		n := len(t.cols)
		if t.ordered {
			n++
		}
		stmt, err := tx.Prepare(`INSERT OR REPLACE INTO ` + t.name + ` (` + t.colList() + `) VALUES (?` + strings.Repeat(", ?", n-1) + `)`)
		if err != nil {
			return nil, err
		}
		defer stmt.Close()
		for _, w := range writes {
			args := values(t.fields(&w.v))
			if t.ordered {
				args = append(args, w.seq)
			}
			if _, err := stmt.Exec(args...); err != nil {
				return nil, err
			}
		}
	}
	if len(gone) > 0 {
		sort.Strings(gone)
		stmt, err := tx.Prepare(`DELETE FROM ` + t.name + ` WHERE "` + t.cols[0] + `" = ?`)
		if err != nil {
			return nil, err
		}
		defer stmt.Close()
		for _, k := range gone {
			if _, err := stmt.Exec(k); err != nil {
				return nil, err
			}
		}
	}
	return func() { t.snap = next }, nil
}

// values dereferences a row's field pointers into arguments.
func values(ptrs []any) []any {
	out := make([]any, len(ptrs))
	for i, p := range ptrs {
		switch p := p.(type) {
		case *string:
			out[i] = *p
		case *int64:
			out[i] = *p
		case *bool:
			out[i] = *p
		default:
			panic(fmt.Sprintf("store: column type %T", p))
		}
	}
	return out
}

// assignRow and markRow are the map tables as rows.
type assignRow struct {
	Window string
	Assignment
}

type markRow struct {
	ID string
	At int64
}

// stashKey names a stash's row: a composer's draft by its id, any other
// stash by its text, which StashPrompt keeps unique.
func stashKey(p StashedPrompt) string {
	if p.DraftID != "" {
		return "draft:" + p.DraftID
	}
	return "text:" + p.Text
}

// tables is the description of every mirrored table, embedded in Store.
type tables struct {
	projectT  table[Project]
	taskT     table[Task]
	assignT   table[assignRow]
	contextT  table[ContextItem]
	reminderT table[Reminder]
	noteT     table[Note]
	presetT   table[Preset]
	queueT    table[QueuedPrompt]
	markT     table[markRow]
	stashT    table[stashedRow]
}

func newTables() tables {
	return tables{
		projectT: table[Project]{name: "projects", ordered: true,
			cols: []string{"id", "name", "theme_color", "theme_glyph", "status", "created", "repo", "worktree"},
			fields: func(p *Project) []any {
				return []any{&p.ID, &p.Name, &p.Theme.Color, &p.Theme.Glyph, &p.Status, &p.Created, &p.Repo, &p.Worktree}
			}},
		taskT: table[Task]{name: "tasks", ordered: true,
			cols: []string{"id", "parent_id", "project_id", "title", "theme_color", "theme_glyph", "status", "easy", "snoozed_until", "created", "updated"},
			fields: func(t *Task) []any {
				return []any{&t.ID, &t.ParentID, &t.ProjectID, &t.Title, &t.Theme.Color, &t.Theme.Glyph, &t.Status, &t.Easy, &t.SnoozedUntil, &t.Created, &t.Updated}
			}},
		assignT: table[assignRow]{name: "assignments",
			cols: []string{"window", "session", "task_id", "project_id"},
			fields: func(a *assignRow) []any {
				return []any{&a.Window, &a.Session, &a.TaskID, &a.ProjectID}
			}},
		contextT: table[ContextItem]{name: "context_items", ordered: true,
			cols: []string{"id", "kind", "text", "session", "window", "pane", "project_id", "task_id", "starred", "pinned", "created"},
			fields: func(c *ContextItem) []any {
				return []any{&c.ID, &c.Kind, &c.Text, &c.Session, &c.Window, &c.Pane, &c.ProjectID, &c.TaskID, &c.Starred, &c.Pinned, &c.Created}
			}},
		reminderT: table[Reminder]{name: "reminders", ordered: true,
			cols: []string{"id", "title", "message", "interval", "enabled", "next", "last", "created"},
			fields: func(r *Reminder) []any {
				return []any{&r.ID, &r.Title, &r.Message, &r.Interval, &r.Enabled, &r.Next, &r.Last, &r.Created}
			}},
		noteT: table[Note]{name: "notes", ordered: true,
			cols: []string{"id", "text", "task_id", "project_id", "created", "updated"},
			fields: func(n *Note) []any {
				return []any{&n.ID, &n.Text, &n.TaskID, &n.ProjectID, &n.Created, &n.Updated}
			}},
		presetT: table[Preset]{name: "prompt_presets", ordered: true,
			cols: []string{"id", "name", "prefix", "cwd", "created"},
			fields: func(p *Preset) []any {
				return []any{&p.ID, &p.Name, &p.Prefix, &p.Cwd, &p.Created}
			}},
		queueT: table[QueuedPrompt]{name: "queued_prompts", ordered: true,
			cols: []string{"id", "text", "preset", "task_id", "project_id", "cwd", "session", "agent", "window", "created"},
			fields: func(q *QueuedPrompt) []any {
				return []any{&q.ID, &q.Text, &q.Preset, &q.TaskID, &q.ProjectID, &q.Cwd, &q.Session, &q.Agent, &q.Window, &q.Created}
			}},
		markT: table[markRow]{name: "review_marks",
			cols: []string{"id", "reviewed_at"},
			fields: func(m *markRow) []any {
				return []any{&m.ID, &m.At}
			}},
		stashT: table[stashedRow]{name: "prompt_stashes", ordered: true, reverse: true,
			cols: []string{"stash_key", "draft_id", "text", "saved"},
			fields: func(p *stashedRow) []any {
				return []any{&p.Key, &p.DraftID, &p.Text, &p.Saved}
			}},
	}
}

// stashedRow is a stash with its key, the column the table is keyed by.
type stashedRow struct {
	Key string
	StashedPrompt
}

const historyCols = `id, kind, at, observed, source, source_id, session, "window", summary, detail`

func historyFields(e *HistoryEntry) []any {
	return []any{&e.ID, &e.Kind, &e.At, &e.Observed, &e.Source, &e.SourceID, &e.Session, &e.Window, &e.Summary, &e.Detail}
}

// insertHistory appends entries; one another deck already wrote is kept as
// it was, as AddHistory keeps a re-read entry.
func insertHistory(tx *sql.Tx, entries []HistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO history (` + historyCols + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i := range entries {
		if _, err := stmt.Exec(values(historyFields(&entries[i]))...); err != nil {
			return err
		}
	}
	return nil
}

func deleteHistory(tx *sql.Tx, entries []HistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`DELETE FROM history WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		if _, err := stmt.Exec(e.ID); err != nil {
			return err
		}
	}
	return nil
}
