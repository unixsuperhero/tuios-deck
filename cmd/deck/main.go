// Command deck is a terminal dashboard over the tuios daemon: it lists every
// terminal and agent, starts new ones with a prompt, queues prompts, follows
// each pane's screen, activity and transcript, and saves captures to files.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/capture"
	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/ui"
)

var version = "dev"

func main() {
	socket := flag.String("socket", "", "daemon socket (default $TUIOS_SOCKET or the per-user runtime folder)")
	captures := flag.String("captures", "", "folder for saved captures (default "+capture.Dir()+")")
	flag.Parse()

	client := &daemon.Client{Socket: *socket}
	if client.Socket == "" {
		client.Socket = daemon.DefaultSocket()
	}
	dir := *captures
	if dir == "" {
		dir = capture.Dir()
	}

	m := ui.New(client, version, dir)
	defer m.Close()
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "deck:", err)
		os.Exit(1)
	}
}
