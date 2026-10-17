package web

import (
	"context"
	"net/http"
	"strconv"

	"github.com/coder/websocket"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
)

// stream bridges a WebSocket to stream-pane. Frames pass through whole in
// both directions: the page parses the daemon's S, O, R, E and X frames and
// builds its own I and L frames.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cols, _ := strconv.Atoi(q.Get("cols"))
	rows, _ := strconv.Atoi(q.Get("rows"))
	fromSeq, _ := strconv.ParseInt(q.Get("from_seq"), 10, 64)
	st, err := s.Client.StreamPane(r.Context(), daemon.StreamParams{
		Session: r.PathValue("session"), Window: r.PathValue("id"),
		FromSeq: fromSeq, BootID: q.Get("boot_id"), LeaseCols: cols, LeaseRows: rows,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		st.Close()
		return
	}
	defer c.CloseNow()
	defer st.Close()
	c.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// The reply line first, as text, so the page knows the mode, size and boot id.
	reply, _ := marshal(st.Reply)
	if err := c.Write(ctx, websocket.MessageText, reply); err != nil {
		return
	}
	go func() {
		defer cancel()
		for {
			frame, err := st.ReadFrame()
			if err != nil {
				return
			}
			if err := c.Write(ctx, websocket.MessageBinary, frame); err != nil {
				return
			}
		}
	}()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary {
			continue
		}
		if err := st.WriteFrame(data); err != nil {
			Log.Println("client frame dropped:", err)
			continue
		}
	}
}
