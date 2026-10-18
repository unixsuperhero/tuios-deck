// Package reminders delivers the store's recurring reminders when they fall
// due. It runs beside a front end, only while the deck runs, and never
// touches its view.
package reminders

import (
	"context"
	"os/exec"
	"runtime"
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

const (
	checkEvery  = time.Second
	callTimeout = 10 * time.Second
)

// Run checks the store every second until ctx ends and delivers each due
// reminder twice over: as a notice in the daemon's session, which a tuios
// client shows, and as an OS notification when os is true. A failed
// delivery is reported through log (which may be nil) and not retried; the
// schedule has already moved on.
func Run(ctx context.Context, client *daemon.Client, st *store.Store, os bool, log func(string)) {
	if log == nil {
		log = func(string) {}
	}
	tk := time.NewTicker(checkEvery)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-tk.C:
			due, err := st.TakeDue(t.Unix())
			if err != nil {
				log("reminders: " + err.Error())
			}
			for _, r := range due {
				deliver(ctx, client, r, os, log)
			}
		}
	}
}

func deliver(ctx context.Context, client *daemon.Client, r store.Reminder, os bool, log func(string)) {
	text := r.Message
	if text == "" {
		text = r.Title
	}
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := client.Notice(cctx, "", "reminder: "+r.Title, text); err != nil {
		log("reminder " + r.Title + ": notice: " + err.Error())
	} else {
		log("reminder " + r.Title + ": delivered")
	}
	if os {
		if err := osNotify(cctx, r.Title, text); err != nil {
			log("reminder " + r.Title + ": os notification: " + err.Error())
		}
	}
}

// osNotify shows a desktop notification. The text reaches the program as
// an argument, never through a shell.
func osNotify(ctx context.Context, title, text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "osascript",
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			"--", title, text)
	case "linux":
		cmd = exec.CommandContext(ctx, "notify-send", "--", title, text)
	default:
		return nil
	}
	return cmd.Run()
}
