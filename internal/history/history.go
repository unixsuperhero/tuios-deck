// Package history persists the daemon's retained activity and live lifecycle
// events. It runs beside the front end and never changes its selection or scroll.
package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

const Boundary = "History keeps deck submission acknowledgements, observed events and retained agent activity / shell commands. Queue acceptance is not delivery. Offline gaps, expired rings and unreported prompts cannot be recovered. A turn-end note is not necessarily the full response."

// eventTypes leaves out window-retitled on purpose: harnesses animate pane
// titles, a spinner frame per event, which once made retitles 98% of a store's
// history and rewrote the whole file a dozen times a second.
var eventTypes = []string{"agent-activity", "command-started", "command-finished", "window-created", "window-closed", "window-exit", "window-moved", "window-minimized", "window-restored", "session-created", "session-closed", "gap"}

// flushEvery bounds how often live events are written. Every save rewrites
// the whole store, so an event more than a second after the last save is
// written at once, and the ones that follow within that second share one
// save when it ends. A lone event is never delayed; only a storm is batched.
const flushEvery = time.Second

// Run subscribes before recovering retained sources, so recovery cannot miss
// events that arrive while the sources are being read. Stable IDs deduplicate both.
func Run(ctx context.Context, client *daemon.Client, st *store.Store) {
	for ctx.Err() == nil {
		err := watch(ctx, client, st)
		if ctx.Err() != nil {
			return
		}
		st.SetHistoryError("history observation interrupted: " + err.Error())
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func watch(ctx context.Context, client *daemon.Client, st *store.Store) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, err := client.Subscribe(ctx, "", eventTypes)
	if err != nil {
		return err
	}
	var pending []store.HistoryEntry
	var lastSave time.Time
	var due <-chan time.Time // armed while a batch waits; nil blocks the case
	flush := func() error {
		due = nil
		lastSave = time.Now()
		if len(pending) == 0 {
			return nil
		}
		err := st.AddHistory(pending...)
		pending = pending[:0]
		return err
	}
	for {
		select {
		case <-ctx.Done():
			flush()
			return ctx.Err()
		case <-due:
			if err := flush(); err != nil {
				return err
			}
		case ev, ok := <-events:
			if !ok {
				flush()
				return fmt.Errorf("daemon stream closed; retained sources will be read on reconnect")
			}
			if ev.Type == "subscribed" {
				err = recoverSources(ctx, client, st, ev.BootID)
				lastSave = time.Now()
			} else {
				var entries []store.HistoryEntry
				entries, err = observe(ctx, client, st, ev)
				pending = append(pending, entries...)
				if err == nil && len(pending) > 0 {
					// A harness report (a prompt, a turn end) is rare and is
					// what the person waits to see, so it is written at once;
					// command and window events come in storms and wait.
					urgent := ev.Type == "agent-activity"
					if wait := flushEvery - time.Since(lastSave); urgent || wait <= 0 || len(pending) >= 1000 {
						err = flush()
					} else if due == nil {
						due = time.After(wait)
					}
				}
			}
			if err != nil {
				return err
			}
			st.SetHistoryError("")
		}
	}
}

func recoverSources(ctx context.Context, client *daemon.Client, st *store.Store, boot string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	sessions, err := panes.List(ctx, client)
	if err != nil {
		return err
	}
	var entries []store.HistoryEntry
	for _, s := range sessions {
		for _, p := range s.Panes {
			a, err := client.Activity(ctx, s.Name, p.ID, 256)
			if err != nil {
				if vanished(err) {
					continue
				}
				return err
			}
			for _, e := range a.Entries {
				if h, ok := activity(boot, s.Name, p.ID, e); ok {
					entries = append(entries, h)
				}
			}
			cmds, err := client.CommandHistory(ctx, s.Name, p.ID, 200)
			if err != nil {
				if vanished(err) {
					continue
				}
				return err
			}
			for _, c := range cmds {
				entries = append(entries, command(boot, c)...)
			}
		}
	}
	return st.AddHistory(entries...)
}

func activity(boot, session, window string, e daemon.ActivityEntry) (store.HistoryEntry, bool) {
	kind, label := "", ""
	switch e.Kind {
	case "prompt":
		kind, label = "prompt-submitted", "Prompt submitted"
	case "turn_end":
		kind, label = "response-completed", "Agent response completed"
	case "state":
		kind, label = "agent-state", "Agent state"
	case "session_start", "subagent_start", "subagent_stop":
		kind, label = e.Kind, e.Kind
	default:
		return store.HistoryEntry{}, false
	}
	sourceID := fmt.Sprintf("%s/%s/%d/%d", boot, window, e.Seq, e.At)
	detail := e.Text
	if e.Kind == "turn_end" {
		detail = "Turn-end hook report (may be a summary, not the full response):\n\n" + detail
		if e.Text == "" {
			detail += "The harness reported completion without response text."
		}
	}
	return store.HistoryEntry{ID: "activity/" + sourceID, Source: "agent-activity", SourceID: sourceID,
		Kind: kind, At: e.At / int64(time.Millisecond), Session: session, Window: window,
		Summary: label + ": " + panes.OneLine(e.Text), Detail: detail}, true
}

func command(boot string, c daemon.HistoryEntry) []store.HistoryEntry {
	sourceID := fmt.Sprintf("%s/%s/%d/%d", boot, c.Window, c.Seq, c.At)
	exit := "not reported"
	if c.Exit != nil {
		exit = fmt.Sprint(*c.Exit)
	}
	detail := fmt.Sprintf("%s\n\nWorking directory: %s\nExit: %s\nDuration: %d ms\nCommand sequence: %d", c.Cmdline, c.Cwd, exit, c.DurationMS, c.Seq)
	return []store.HistoryEntry{
		{ID: "command-start/" + sourceID, Source: "command-history", SourceID: sourceID,
			Kind: "command-submitted", At: c.At, Session: c.Session, Window: c.Window, Summary: "Command submitted (retained): " + c.Cmdline, Detail: c.Cmdline + "\n\nStart time retained by shell integration."},
		{ID: "command-end/" + sourceID, Source: "command-history", SourceID: sourceID,
			Kind: "command-completed", At: c.At + c.DurationMS, Session: c.Session, Window: c.Window, Summary: "Command completed (exit " + exit + "): " + c.Cmdline, Detail: detail},
	}
}

// observe turns one live event into the entries it records. The caller
// batches them into one save; only a gap saves here, through recovery.
func observe(ctx context.Context, client *daemon.Client, st *store.Store, ev daemon.Event) ([]store.HistoryEntry, error) {
	var raw struct {
		Time       int64                `json:"time"`
		Entry      daemon.ActivityEntry `json:"entry"`
		Cmdline    string               `json:"cmdline"`
		CommandSeq int64                `json:"command_seq"`
	}
	if err := json.Unmarshal(ev.Raw, &raw); err != nil {
		return nil, err
	}
	if ev.Type == "agent-activity" {
		if h, ok := activity(ev.BootID, ev.Session, ev.Window, raw.Entry); ok {
			return []store.HistoryEntry{h}, nil
		}
		return nil, nil
	}
	var entries []store.HistoryEntry
	if ev.Type == "command-finished" {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmds, err := client.CommandHistory(cctx, ev.Session, ev.Window, 200)
		if err != nil && !vanished(err) {
			return nil, err
		}
		for _, c := range cmds {
			entries = append(entries, command(ev.BootID, c)...)
		}
		// A pane may close before its command ring can be read. Preserve the event
		// when the matching command has already left the ring instead of inventing it.
		for _, c := range cmds {
			if c.Seq == raw.CommandSeq {
				return entries, nil
			}
		}
	}
	sourceID := fmt.Sprintf("%s/%d", ev.BootID, ev.Seq)
	h := store.HistoryEntry{ID: "event/" + sourceID, Source: "daemon", SourceID: sourceID,
		Kind: ev.Type, At: raw.Time / int64(time.Millisecond), Session: ev.Session, Window: ev.Window,
		Summary: strings.ReplaceAll(ev.Type, "-", " "), Detail: string(ev.Raw)}
	if h.At == 0 {
		h.At = time.Now().UnixMilli()
		h.Detail = "Source has no timestamp; time is when observed.\n" + h.Detail
	}
	if ev.Type == "command-started" {
		h.Summary = "Command started (observed): " + raw.Cmdline
	}
	if ev.Type == "command-finished" {
		h.Kind = "command-completed"
	}
	if ev.Type == "gap" {
		h.Summary = "History gap: " + ev.Reason
	}
	entries = append(entries, h)
	if ev.Type == "gap" {
		if err := st.AddHistory(entries...); err != nil {
			return nil, err
		}
		return nil, recoverSources(ctx, client, st, ev.BootID)
	}
	return entries, nil
}

func vanished(err error) bool {
	var e *daemon.Error
	return errors.As(err, &e) && (e.Code == "window_not_found" || e.Code == "session_not_found")
}
