// Package daemon is a small client for the tuios daemon's JSON verb protocol:
// one JSON object per line over the daemon's unix socket, one reply line per
// request. It opens one connection per call, so calls may run concurrently.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// Client talks to one daemon socket.
type Client struct {
	Socket string
}

// DefaultSocket is the socket the tuios CLI would use from this process:
// $TUIOS_SOCKET, else the per-user runtime folder.
func DefaultSocket() string {
	if s := os.Getenv("TUIOS_SOCKET"); s != "" {
		return s
	}
	uid := strconv.Itoa(os.Getuid())
	candidates := []string{}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		candidates = append(candidates, filepath.Join(d, "tuios", "tuios.sock"))
	}
	candidates = append(candidates,
		filepath.Join("/run/user", uid, "tuios", "tuios.sock"),
		filepath.Join(os.TempDir(), "tuios-"+uid, "tuios.sock"),
		"/tmp/tuios-"+uid+"/tuios.sock",
	)
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return candidates[len(candidates)-1]
}

// Error is a verb failure: a stable code, a message, and the daemon's hint.
type Error struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Hint    json.RawMessage `json:"hint,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

type envelope struct {
	ID     any             `json:"id,omitempty"`
	Verb   string          `json:"verb,omitempty"`
	Params any             `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", c.Socket, err)
	}
	// Cancelling the context closes the connection, which ends any blocked read.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	return &ctxConn{Conn: conn, stop: stop}, nil
}

type ctxConn struct {
	net.Conn
	stop func() bool
}

func (c *ctxConn) Close() error { c.stop(); return c.Conn.Close() }

// request writes one verb and reads its reply line on an open connection.
func request(conn net.Conn, r *bufio.Reader, verb string, params any) (json.RawMessage, error) {
	line, err := json.Marshal(envelope{ID: 1, Verb: verb, Params: params})
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return nil, fmt.Errorf("%s: write: %w", verb, err)
	}
	reply, err := r.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("%s: read reply: %w (an older daemon closes the connection on a JSON request)", verb, err)
	}
	var env envelope
	if err := json.Unmarshal(reply, &env); err != nil {
		return nil, fmt.Errorf("%s: decode reply: %w", verb, err)
	}
	if env.Error != nil {
		return nil, env.Error
	}
	return env.Result, nil
}

// Call sends one verb and decodes its result into out, which may be nil.
func (c *Client) Call(ctx context.Context, verb string, params any, out any) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	res, err := request(conn, bufio.NewReaderSize(conn, 1<<16), verb, params)
	if err != nil {
		return err
	}
	if out == nil || len(res) == 0 {
		return nil // a verb with nothing to report leaves out untouched
	}
	if err := json.Unmarshal(res, out); err != nil {
		return fmt.Errorf("%s: decode result: %w", verb, err)
	}
	return nil
}

// Event is one line of the subscribe stream.
type Event struct {
	Type    string          `json:"type"`
	Seq     int64           `json:"seq"`
	BootID  string          `json:"boot_id"`
	Session string          `json:"session"`
	Window  string          `json:"window"`
	State   string          `json:"state"`
	Reason  string          `json:"reason"`
	Raw     json.RawMessage `json:"-"`
}

// Subscribe opens the event stream. The channel closes when the stream ends;
// cancel ctx to end it.
func (c *Client) Subscribe(ctx context.Context, session string, types []string) (<-chan Event, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(conn, 1<<16)
	params := map[string]any{}
	if session != "" {
		params["session"] = session
	}
	if len(types) > 0 {
		params["types"] = types
	}
	if _, err := request(conn, r, "subscribe", params); err != nil {
		conn.Close()
		return nil, err
	}
	ch := make(chan Event, 64)
	go func() {
		defer close(ch)
		defer conn.Close()
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var ev Event
			if json.Unmarshal(line, &ev) != nil {
				continue
			}
			ev.Raw = append(json.RawMessage(nil), line...)
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// Presence asks for the person's nonce with attach-presence. The nonce lives
// as long as the connection stays open: cancel ctx to release it, and read
// lost to learn that the daemon ended it (it closes when it went away). It
// is refused from inside a tuios pane.
func (c *Client) Presence(ctx context.Context) (nonce string, lost <-chan struct{}, err error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return "", nil, err
	}
	r := bufio.NewReaderSize(conn, 1<<16)
	res, err := request(conn, r, "attach-presence", map[string]any{})
	if err != nil {
		conn.Close()
		return "", nil, err
	}
	var out struct {
		Nonce string `json:"human_nonce"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		conn.Close()
		return "", nil, err
	}
	done := make(chan struct{})
	go func() {
		// The daemon writes nothing more on this connection, so a read ends
		// only when the connection does.
		defer close(done)
		defer conn.Close()
		var b [1]byte
		conn.Read(b[:])
	}()
	return out.Nonce, done, nil
}
