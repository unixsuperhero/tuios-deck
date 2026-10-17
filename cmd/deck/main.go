// Command deck is the tuios deck: a dashboard over the tuios daemon as a
// terminal program (deck tui) and a web page (deck web), and the setup of
// the files the deck needs on a machine (deck setup).
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/capture"
	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/ui"
	"github.com/unixsuperhero/tuios-deck/internal/web"
	"github.com/unixsuperhero/tuios-deck/setup"
)

var version = "dev"

const usage = `deck: terminals and agents over the tuios daemon

Usage:
  deck <command> [flags]

Commands:
  tui       The dashboard in this terminal
  web       The dashboard as a web page (default http://127.0.0.1:8787)
  setup     Install, check or remove the files the deck needs on this machine
  version   Print the version

Run "deck <command> -h" for the flags of a command.
`

const setupUsage = `deck setup: the files the deck needs on this machine

Usage:
  deck setup <command> [item...]

Commands:
  list                 What can be installed, and where each file goes
  status  [item...]    Whether each file is missing, current, outdated or foreign
  install [item...]    Write the files (all of them with no item); --force replaces a foreign file
  uninstall [item...]  Remove the files the deck wrote, and nothing else
  print   <item>       Print the file the binary carries, for inspection

Items:
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "tui":
		err = runTUI(args)
	case "web":
		err = runWeb(args)
	case "setup":
		err = runSetup(args)
	case "version", "--version", "-v":
		fmt.Println("deck", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "deck: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "deck:", err)
		os.Exit(1)
	}
}

// common holds the flags every daemon-facing command takes.
type common struct {
	socket   string
	captures string
}

func (c *common) bind(fs *flag.FlagSet) {
	fs.StringVar(&c.socket, "socket", "", "daemon socket (default $TUIOS_SOCKET or the per-user runtime folder)")
	fs.StringVar(&c.captures, "captures", "", "folder for saved captures (default "+capture.Dir()+")")
}

func (c *common) resolve() (*daemon.Client, string) {
	client := &daemon.Client{Socket: c.socket}
	if client.Socket == "" {
		client.Socket = daemon.DefaultSocket()
	}
	dir := c.captures
	if dir == "" {
		dir = capture.Dir()
	}
	return client, dir
}

func runTUI(args []string) error {
	fs := flag.NewFlagSet("deck tui", flag.ExitOnError)
	var c common
	c.bind(fs)
	fs.Parse(args)
	client, dir := c.resolve()
	m := ui.New(client, version, dir)
	defer m.Close()
	_, err := tea.NewProgram(m).Run()
	return err
}

func runWeb(args []string) error {
	fs := flag.NewFlagSet("deck web", flag.ExitOnError)
	var c common
	c.bind(fs)
	listen := fs.String("listen", "127.0.0.1:8787", "address to serve on")
	allowRemote := fs.Bool("allow-remote", false, "serve on an address other than loopback; there is no password, so put it behind something that has one")
	fs.Parse(args)

	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return fmt.Errorf("bad --listen: %w", err)
	}
	if ip := net.ParseIP(host); (ip == nil || !ip.IsLoopback()) && host != "localhost" && !*allowRemote {
		return fmt.Errorf("--listen is not loopback and there is no password; pass --allow-remote to serve it anyway")
	}
	client, dir := c.resolve()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	s := &web.Server{Client: client, Version: version, CaptureDir: dir}
	s.HoldPresence(ctx)
	srv := &http.Server{Addr: *listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()
	web.Log.Printf("serving http://%s for daemon %s", *listen, client.Socket)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func runSetup(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printSetupUsage()
		if len(args) == 0 {
			os.Exit(2)
		}
		return nil
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("deck setup "+sub, flag.ExitOnError)
	force := fs.Bool("force", false, "install: replace a file the deck did not write")
	fs.Parse(rest)
	items, err := pick(fs.Args())
	if err != nil {
		return err
	}
	failed := 0
	switch sub {
	case "list":
		for _, it := range setup.Items() {
			fmt.Printf("%-14s %s\n%-14s → %s\n", it.ID, it.Title, "", it.Target)
			for _, n := range it.Needs {
				fmt.Printf("%-14s   needs %s\n", "", n.What)
			}
			if it.Notes != "" {
				fmt.Printf("%-14s   %s\n", "", it.Notes)
			}
		}
	case "status":
		for _, it := range items {
			st, err := it.Status()
			if err != nil {
				fmt.Printf("%-14s error: %v\n", it.ID, err)
				failed++
				continue
			}
			fmt.Printf("%-14s %-9s %s\n", it.ID, st, it.Path())
			for _, n := range it.UnmetNeeds() {
				fmt.Printf("%-14s   missing: %s. %s\n", "", n.What, n.Hint)
			}
			if st != setup.Current {
				failed++
			}
		}
	case "install":
		for _, it := range items {
			done, err := it.Install(*force)
			if err != nil {
				fmt.Printf("%-14s FAILED %v\n", it.ID, err)
				failed++
				continue
			}
			fmt.Printf("%-14s %s  %s\n", it.ID, done, it.Path())
			for _, n := range it.UnmetNeeds() {
				fmt.Printf("%-14s   still missing: %s. %s\n", "", n.What, n.Hint)
			}
			if it.Notes != "" {
				fmt.Printf("%-14s   %s\n", "", it.Notes)
			}
		}
	case "uninstall":
		for _, it := range items {
			done, err := it.Uninstall()
			if err != nil {
				fmt.Printf("%-14s FAILED %v\n", it.ID, err)
				failed++
				continue
			}
			fmt.Printf("%-14s %s  %s\n", it.ID, done, it.Path())
		}
	case "print":
		if len(fs.Args()) != 1 {
			return fmt.Errorf("print takes exactly one item")
		}
		os.Stdout.Write(items[0].Content())
	default:
		printSetupUsage()
		return fmt.Errorf("unknown setup command %q", sub)
	}
	if failed > 0 {
		os.Exit(1)
	}
	return nil
}

func printSetupUsage() {
	fmt.Print(setupUsage)
	for _, it := range setup.Items() {
		fmt.Printf("  %-12s %s\n", it.ID, it.Title)
	}
}

// pick resolves item ids; none means every item.
func pick(ids []string) ([]setup.Item, error) {
	if len(ids) == 0 {
		return setup.Items(), nil
	}
	var out []setup.Item
	for _, id := range ids {
		it, ok := setup.Find(id)
		if !ok {
			var known []string
			for _, k := range setup.Items() {
				known = append(known, k.ID)
			}
			return nil, fmt.Errorf("unknown item %q; items: %s", id, strings.Join(known, ", "))
		}
		out = append(out, it)
	}
	return out, nil
}
