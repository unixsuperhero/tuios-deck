package daemon

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
)

// StreamReply is the one JSON line stream-pane answers before the frames.
type StreamReply struct {
	Mode    string `json:"mode"`
	Seq     int64  `json:"seq"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	BootID  string `json:"boot_id"`
	Session string `json:"session"`
	Window  string `json:"window"`
	Title   string `json:"title"`
}

// Stream is an open stream-pane connection: frames flow both ways.
type Stream struct {
	Reply StreamReply
	conn  net.Conn
	r     *bufio.Reader
}

// StreamParams are the inputs of stream-pane.
type StreamParams struct {
	Session   string
	Window    string
	FromSeq   int64
	BootID    string
	LeaseCols int
	LeaseRows int
}

// StreamPane opens a pane as bytes. Close the stream to release the lease.
func (c *Client) StreamPane(ctx context.Context, p StreamParams) (*Stream, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	params := map[string]any{"window": p.Window}
	if p.Session != "" {
		params["session"] = p.Session
	}
	if p.FromSeq > 0 && p.BootID != "" {
		params["from_seq"], params["boot_id"] = p.FromSeq, p.BootID
	}
	if p.LeaseCols > 0 && p.LeaseRows > 0 {
		params["lease_cols"], params["lease_rows"], params["fit"] = p.LeaseCols, p.LeaseRows, true
	}
	r := bufio.NewReaderSize(conn, 1<<16)
	res, err := request(conn, r, "stream-pane", params)
	if err != nil {
		conn.Close()
		return nil, err
	}
	s := &Stream{conn: conn, r: r}
	if err := json.Unmarshal(res, &s.Reply); err != nil {
		conn.Close()
		return nil, err
	}
	return s, nil
}

// ReadFrame returns the next frame from the daemon, whole: the type byte,
// the length and the payload, as the wire carries it.
func (s *Stream) ReadFrame() ([]byte, error) {
	var head [5]byte
	if _, err := io.ReadFull(s.r, head[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(head[1:])
	if n > 64<<20 {
		return nil, fmt.Errorf("frame of %d bytes is over the cap", n)
	}
	frame := make([]byte, 5+int(n))
	copy(frame, head[:])
	if _, err := io.ReadFull(s.r, frame[5:]); err != nil {
		return nil, err
	}
	return frame, nil
}

// WriteFrame sends one client frame: 'I' with input bytes, or 'L' with a
// u16 cols and u16 rows lease.
func (s *Stream) WriteFrame(frame []byte) error {
	if len(frame) < 5 || (frame[0] != 'I' && frame[0] != 'L') {
		return fmt.Errorf("client frame must be I or L with a header")
	}
	if int(binary.BigEndian.Uint32(frame[1:5])) != len(frame)-5 {
		return fmt.Errorf("client frame length does not match its payload")
	}
	if len(frame)-5 > 64<<10 {
		return fmt.Errorf("client frame over 64 KiB")
	}
	_, err := s.conn.Write(frame)
	return err
}

// Close ends the stream.
func (s *Stream) Close() error { return s.conn.Close() }
