package wsapi

import (
	"errors"
	"testing"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

// FuzzDecode guards the one function that turns arbitrary bytes off the wire
// into a message the rest of the server trusts. The invariant Decode's own
// comment promises is narrower than "never panics" — a non-binary frame is
// always ErrNotBinary, and anything else either fails cleanly or comes back
// as a real message — but a panic here would take the reader goroutine down
// with a client that sent nothing but noise, so the fuzzer is left free to
// find one if it can.
func FuzzDecode(f *testing.F) {
	for _, m := range clientVariants() {
		raw, err := proto.Marshal(m)
		if err != nil {
			f.Fatalf("seed marshal: %v", err)
		}
		f.Add(int(websocket.MessageBinary), raw)
	}
	f.Add(int(websocket.MessageText), []byte("not protobuf"))
	f.Add(int(websocket.MessageBinary), []byte{})
	f.Add(int(websocket.MessageBinary), []byte{0xff, 0xff, 0xff, 0x01})
	f.Add(0, []byte{0x01, 0x02})

	f.Fuzz(func(t *testing.T, typ int, raw []byte) {
		msg, err := Decode(websocket.MessageType(typ), raw)

		if websocket.MessageType(typ) != websocket.MessageBinary {
			if !errors.Is(err, ErrNotBinary) {
				t.Fatalf("Decode(typ=%d, ...) err = %v, want ErrNotBinary", typ, err)
			}
			return
		}
		if err == nil && msg == nil {
			t.Fatalf("Decode(%q) returned neither an error nor a message", raw)
		}
	})
}
