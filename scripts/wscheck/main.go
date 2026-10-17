// Command wscheck is the e2e probe of the deck-web stream: it opens the
// WebSocket of one pane, expects the reply line and a snapshot, types a
// command through an I frame, and waits for its output in O frames.
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: wscheck ws://host/api/panes/S/W/stream?cols=80&rows=24 MARKER")
		os.Exit(2)
	}
	url, marker := os.Args[1], os.Args[2]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		fmt.Println("FAIL dial:", err)
		os.Exit(1)
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	typ, data, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageText || !strings.Contains(string(data), `"mode"`) {
		fmt.Println("FAIL reply line:", err, string(data))
		os.Exit(1)
	}
	fmt.Println("PASS stream reply:", strings.TrimSpace(string(data)))
	typ, data, err = c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || data[0] != 'S' {
		fmt.Println("FAIL first frame is not a snapshot:", err)
		os.Exit(1)
	}
	cols, rows := binary.BigEndian.Uint16(data[13:]), binary.BigEndian.Uint16(data[15:])
	fmt.Printf("PASS snapshot %dx%d, %d bytes\n", cols, rows, len(data)-5-12)

	// Type a command through an I frame.
	input := []byte("echo " + marker + "\n")
	frame := make([]byte, 5+len(input))
	frame[0] = 'I'
	binary.BigEndian.PutUint32(frame[1:], uint32(len(input)))
	copy(frame[5:], input)
	if err := c.Write(ctx, websocket.MessageBinary, frame); err != nil {
		fmt.Println("FAIL write I frame:", err)
		os.Exit(1)
	}
	var seen strings.Builder
	for {
		typ, data, err = c.Read(ctx)
		if err != nil {
			fmt.Println("FAIL waiting for output:", err, "saw:", seen.String())
			os.Exit(1)
		}
		if typ != websocket.MessageBinary {
			continue
		}
		switch data[0] {
		case 'O':
			seen.Write(data[13:])
			// The echo of the typed line comes first; the marker on its own line is the output.
			if strings.Count(seen.String(), marker) >= 2 {
				fmt.Println("PASS output of the typed command arrived through O frames")
				return
			}
		case 'X':
			fmt.Println("FAIL stream ended:", string(data[5:]))
			os.Exit(1)
		}
	}
}
