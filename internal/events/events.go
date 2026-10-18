// Package events holds the one daemon event stream a deck process keeps,
// and fans it out: to the handlers that record things in the background
// (auto-capture, history) and to the taps a front end opens (the TUI once,
// the web page once per browser tab). The dispatcher dials, subscribes with
// the union of every consumer's types, reconnects with a growing backoff,
// resumes with after_seq and boot_id so a blip loses nothing, and tells
// every handler to re-read its sources when the daemon says events are gone.
package events

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
)

// Handler is one consumer of the daemon's event stream. Its methods are
// called one at a time, in order, on a goroutine of its own, so a slow
// handler never delays another.
type Handler interface {
	// Types is the event types it wants; the union over handlers and taps
	// is the subscribe filter.
	Types() []string
	// Handle is called for every event of those types, in order.
	Handle(ctx context.Context, ev daemon.Event)
	// Resync is called after the first subscribed reply and after every
	// gap, the daemon's or the handler's own (its queue overflowed): re-read
	// whatever rings the handler depends on.
	Resync(ctx context.Context, bootID string)
}

// Closer is what a Handler may add to be told when the stream has ended for
// good: Close is the last call it gets, after ctx ended, and Start's done
// channel waits for it. History flushes its last batch there.
type Closer interface {
	Close()
}

// queueSize is each handler's and each tap's buffer. A handler whose queue
// is full drops the event and is resynced once it catches up; a tap drops.
const queueSize = 256

const (
	minBackoff = time.Second
	maxBackoff = 30 * time.Second
)

// Runner is the dispatcher. New builds it, Tap opens a channel for a front
// end, Start runs it.
type Runner struct {
	client   *daemon.Client
	log      func(string)
	handlers []*worker

	mu     sync.Mutex
	types  map[string]bool
	taps   map[*tap]struct{}
	redial context.CancelFunc // ends the open connection, so the loop dials again with the current types
	ended  bool               // ctx ended; taps opened now close at once

	resume  daemon.Resume // the last seq and boot_id received
	synced  bool          // the first subscribed reply has been seen
	dropped atomic.Int64  // events dropped across handlers and taps
}

// item is one entry of a handler's queue: an event, or a resync marker.
// boot is the boot_id either came with, for the resync a drop calls for.
type item struct {
	ev     daemon.Event
	resync bool
	boot   string
}

type worker struct {
	h      Handler
	types  map[string]bool
	queue  chan item
	gapped atomic.Bool // events were dropped; resync once the handler is back
}

type tap struct {
	types map[string]bool
	ch    chan daemon.Event
}

// New builds the dispatcher for one daemon with its background handlers.
// log may be nil.
func New(client *daemon.Client, log func(string), handlers ...Handler) *Runner {
	if log == nil {
		log = func(string) {}
	}
	r := &Runner{client: client, log: log, types: map[string]bool{"gap": true}, taps: map[*tap]struct{}{}}
	for _, h := range handlers {
		w := &worker{h: h, types: map[string]bool{}, queue: make(chan item, queueSize)}
		for _, t := range h.Types() {
			w.types[t] = true
			r.types[t] = true
		}
		r.handlers = append(r.handlers, w)
	}
	return r
}

// Include widens the subscribe filter by these types without opening a tap,
// for a front end that taps later with them. Called before Start, it costs
// nothing; after, it re-dials once with resume.
func (r *Runner) Include(types ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.widen(types)
}

// widen adds types to the filter and ends the open connection when the
// filter grew. Called with r.mu held.
func (r *Runner) widen(types []string) {
	grew := false
	for _, t := range types {
		if !r.types[t] {
			r.types[t] = true
			grew = true
		}
	}
	if grew && r.redial != nil {
		r.redial()
	}
}

// Tap opens a channel that carries every event of these types, plus every
// subscribed and gap event so the front end knows to list again. The
// channel closes when the dispatcher's ctx ends, or when stop is called;
// never on a reconnect. A tap that falls behind loses events.
func (r *Runner) Tap(types []string) (<-chan daemon.Event, func()) {
	t := &tap{types: map[string]bool{}, ch: make(chan daemon.Event, queueSize)}
	for _, ty := range types {
		t.types[ty] = true
	}
	r.mu.Lock()
	if r.ended {
		r.mu.Unlock()
		close(t.ch)
		return t.ch, func() {}
	}
	r.taps[t] = struct{}{}
	r.widen(types)
	r.mu.Unlock()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			r.mu.Lock()
			_, open := r.taps[t]
			delete(r.taps, t)
			r.mu.Unlock()
			if open {
				close(t.ch)
			}
		})
	}
	return t.ch, stop
}

// Dropped is how many events were dropped so far because a handler or a
// tap fell behind.
func (r *Runner) Dropped() int64 { return r.dropped.Load() }

// Start runs the stream until ctx ends. The returned channel closes once
// every handler has finished (and flushed, through Closer).
func (r *Runner) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	var wg sync.WaitGroup
	for _, w := range r.handlers {
		wg.Add(1)
		go func(w *worker) {
			defer wg.Done()
			r.serve(ctx, w)
		}(w)
	}
	go func() {
		r.loop(ctx)
		for _, w := range r.handlers {
			close(w.queue)
		}
		wg.Wait()
		r.mu.Lock()
		r.ended = true
		for t := range r.taps {
			delete(r.taps, t)
			close(t.ch)
		}
		r.mu.Unlock()
		close(done)
	}()
	return done
}

// serve is one handler's goroutine: its queue in order, then Close.
func (r *Runner) serve(ctx context.Context, w *worker) {
	for it := range w.queue {
		if it.resync {
			w.h.Resync(ctx, it.boot)
		} else {
			w.h.Handle(ctx, it.ev)
		}
		if w.gapped.Swap(false) {
			w.h.Resync(ctx, it.boot)
		}
	}
	if c, ok := w.h.(Closer); ok {
		c.Close()
	}
}

// loop dials and reads until ctx ends, reconnecting with a growing backoff
// and resuming where the last stream stopped.
func (r *Runner) loop(ctx context.Context) {
	backoff := minBackoff
	for ctx.Err() == nil {
		cctx, cancel := context.WithCancel(ctx)
		r.mu.Lock()
		types := make([]string, 0, len(r.types))
		for t := range r.types {
			types = append(types, t)
		}
		resume := r.resume
		r.redial = cancel
		r.mu.Unlock()
		ch, err := r.client.Subscribe(cctx, "", types, &resume)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return
			}
			r.log("events: " + err.Error() + "; retrying in " + backoff.String())
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = minBackoff
		redialed := r.read(ctx, cctx, ch)
		cancel()
		r.mu.Lock()
		r.redial = nil
		r.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		if !redialed {
			r.log("events: stream closed; reconnecting")
		}
	}
}

// read fans one connection's events out until it ends. It reports whether
// the connection was ended on purpose, by Include or Tap widening the filter.
func (r *Runner) read(ctx, cctx context.Context, ch <-chan daemon.Event) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case ev, ok := <-ch:
			if !ok {
				return cctx.Err() != nil && ctx.Err() == nil
			}
			r.dispatch(ev)
		}
	}
}

// dispatch hands one event to the handlers and taps that want it, and turns
// the first subscribed reply and every gap into a resync for every handler.
func (r *Runner) dispatch(ev daemon.Event) {
	if ev.Seq > 0 && ev.BootID != "" {
		r.mu.Lock()
		r.resume = daemon.Resume{AfterSeq: ev.Seq, BootID: ev.BootID}
		r.mu.Unlock()
	}
	resync := false
	switch ev.Type {
	case "subscribed":
		if ev.Replayed > 0 {
			r.log(fmt.Sprintf("events: subscribed, %d events replayed", ev.Replayed))
		}
		r.mu.Lock()
		resync = !r.synced
		r.synced = true
		r.mu.Unlock()
	case "gap":
		r.log("events: gap (" + ev.Reason + "); handlers re-read their sources")
		resync = true
	}
	for _, w := range r.handlers {
		if w.types[ev.Type] {
			r.offer(w, item{ev: ev, boot: ev.BootID})
		}
		if resync {
			r.offer(w, item{resync: true, boot: ev.BootID})
		}
	}
	r.mu.Lock()
	for t := range r.taps {
		if !t.types[ev.Type] && ev.Type != "subscribed" && ev.Type != "gap" {
			continue
		}
		select {
		case t.ch <- ev:
		default:
			r.dropped.Add(1)
		}
	}
	r.mu.Unlock()
}

// offer queues one item for a handler without waiting. A full queue drops
// the item and marks the handler gapped, so it resyncs once it catches up.
func (r *Runner) offer(w *worker, it item) {
	select {
	case w.queue <- it:
	default:
		r.dropped.Add(1)
		if !w.gapped.Swap(true) {
			r.log(fmt.Sprintf("events: %T fell behind; it will re-read its sources", w.h))
		}
	}
}
