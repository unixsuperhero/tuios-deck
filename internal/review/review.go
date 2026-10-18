// Package review collects completed prompt/turn_end pairs from the daemon's
// activity ring. A queued prompt, partial reply or state change is not completion.
package review

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

func Collect(ctx context.Context, client *daemon.Client, st *store.Store) error {
	agents, err := client.ListAgents(ctx)
	if err != nil {
		return err
	}
	var errs []error
	var items []store.Review
	known := map[string]bool{}
	for _, r := range st.Reviews() {
		known[r.ID] = true
	}
	for _, agent := range agents {
		activity, err := client.Activity(ctx, agent.Session, agent.WindowID, 256)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", agent.Name, err))
			continue
		}
		var prompt *daemon.ActivityEntry
		last := -1
		for i := range activity.Entries {
			e := &activity.Entries[i]
			switch e.Kind {
			case "session_start":
				prompt, last = nil, -1
			case "prompt":
				prompt = e
			case "turn_end":
				if prompt == nil {
					continue
				}
				id := fmt.Sprintf("%s-%d-%d", agent.WindowID, prompt.At, prompt.Seq)
				last = -1
				if !known[id] {
					items = append(items, store.Review{
						ID: id, Session: agent.Session, Window: agent.WindowID, Pane: agent.Name, Harness: agent.HarnessID,
						Prompt: prompt.Text, Response: e.Text, StartedAt: prompt.At, CompletedAt: e.At,
					})
					last = len(items) - 1
				}
				prompt = nil
			}
		}
		// Only attach a current screen to the latest finished turn, never an
		// older turn or a prompt that is still running. Recheck the ring after
		// capture so a new prompt racing this read cannot supply its output.
		if last >= 0 && prompt == nil && (agent.State == "idle" || agent.State == "done") {
			shot, err := client.CapturePane(ctx, agent.Session, agent.WindowID, 2000, false)
			if err == nil {
				after, err := client.Activity(ctx, agent.Session, agent.WindowID, 1)
				if err == nil && after.LastSeq == activity.LastSeq {
					items[last].Output = shot.Content
				}
			}
		}
	}
	if err := st.AddReviews(items); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// Run retains completed turns while either frontend runs. It never touches UI
// selection, scroll or status, and works independently of automatic captures.
func Run(ctx context.Context, client *daemon.Client, st *store.Store, log func(string)) {
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		call, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := Collect(call, client, st)
		cancel()
		if err != nil && log != nil {
			log("review: " + err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
