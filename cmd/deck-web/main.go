// Command deck-web serves the deck in a browser: the same terminals and
// agents as the deck TUI, with live panes through xterm.js.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/capture"
	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/web"
)

var version = "dev"

func main() {
	listen := flag.String("listen", "127.0.0.1:8787", "address to serve on")
	socket := flag.String("socket", "", "daemon socket (default $TUIOS_SOCKET or the per-user runtime folder)")
	captures := flag.String("captures", "", "folder for saved captures (default "+capture.Dir()+")")
	allowRemote := flag.Bool("allow-remote", false, "serve on an address other than loopback; there is no password, so put it behind something that has one")
	flag.Parse()

	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "deck-web: bad --listen:", err)
		os.Exit(2)
	}
	if ip := net.ParseIP(host); (ip == nil || !ip.IsLoopback()) && host != "localhost" && !*allowRemote {
		fmt.Fprintln(os.Stderr, "deck-web: --listen is not loopback and there is no password; pass --allow-remote to serve it anyway")
		os.Exit(2)
	}

	client := &daemon.Client{Socket: *socket}
	if client.Socket == "" {
		client.Socket = daemon.DefaultSocket()
	}
	dir := *captures
	if dir == "" {
		dir = capture.Dir()
	}
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
		fmt.Fprintln(os.Stderr, "deck-web:", err)
		os.Exit(1)
	}
}
