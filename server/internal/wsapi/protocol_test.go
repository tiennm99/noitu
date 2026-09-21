package wsapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// The wire contract below the game: the handshake, frame limits, origin
// checking, and the endpoints that answer outside of a room.

// TestProtocolVersionMismatchIsRefused proves an incompatible client is told
// so rather than left to misread frames.
func TestProtocolVersionMismatchIsRefused(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
		ProtocolVersion: ProtocolVersion + 1,
		Nickname:        "Cũ",
	}}})

	if code := c.await("error").GetError().GetCode(); code != "protocol_version_mismatch" {
		t.Errorf("error code = %q, want protocol_version_mismatch", code)
	}
}

// TestHandshakeIsRequiredFirst rejects a client that skips Hello.
func TestHandshakeIsRequiredFirst(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})

	if code := c.await("error").GetError().GetCode(); code != "handshake_required" {
		t.Errorf("error code = %q, want handshake_required", code)
	}
}

// TestOversizeFrameClosesConnection covers the read limit. The frame is built
// past maxFrameBytes, so the socket must close rather than the decoder be
// asked to parse it.
func TestOversizeFrameClosesConnection(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")

	c.submit(strings.Repeat("x", maxFrameBytes+1), 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, _, err := c.conn.Read(ctx)
		if err == nil {
			continue
		}
		// The status matters, not merely that the socket closed: any unrelated
		// failure would otherwise let this pass while the read limit did
		// nothing.
		if got := websocket.CloseStatus(err); got != websocket.StatusMessageTooBig {
			t.Errorf("close status = %v, want StatusMessageTooBig", got)
		}
		return
	}
}

// TestOriginIsChecked confirms a cross-origin handshake is refused when the
// allowlist does not include it.
func TestOriginIsChecked(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{AllowedOrigins: []string{"example.com"}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := websocket.Dial(ctx, url+"/ws", &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Origin": {"http://evil.example"}},
	})
	if err == nil {
		t.Fatal("a disallowed origin completed the handshake")
	}
}

// TestPingIsAnswered covers the clock-offset path the client uses to render a
// countdown against the server's absolute deadline.
func TestPingIsAnswered(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")

	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Ping{
		Ping: &noituv1.Ping{ClientTimeMs: 1234},
	}})

	pong := c.await("pong").GetPong()
	if pong.GetClientTimeMs() != 1234 {
		t.Errorf("pong echoed %d, want 1234", pong.GetClientTimeMs())
	}
	if pong.GetServerTimeMs() == 0 {
		t.Error("pong carried no server clock")
	}
}

// TestTextFrameIsRejected keeps the protocol binary-only: guessing at another
// encoding is how a parser becomes an attack surface.
func TestTextFrameIsRejected(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)

	if err := c.conn.Write(c.ctx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := c.conn.Read(ctx); err != nil {
			return
		}
	}
}

func TestDecodeRejectsTextFrames(t *testing.T) {
	if _, err := Decode(websocket.MessageText, nil); !errors.Is(err, ErrNotBinary) {
		t.Errorf("Decode(text) error = %v, want ErrNotBinary", err)
	}
}

func TestHealthz(t *testing.T) {
	api, _ := newTestServer(t, chainDict(), Config{})
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("healthz returned %d, want 200", rec.Code)
	}
}

func TestErrorMessagesAreUIKeysNotProse(t *testing.T) {
	// Error copy lives in the frontend. A server that sent prose would put
	// Vietnamese strings in two places, and an internal error string would
	// leak server detail to anyone holding a socket.
	m := errorMsg("room_not_found").GetError()
	if strings.ContainsAny(m.GetCode(), " .") {
		t.Errorf("error code %q looks like prose", m.GetCode())
	}
	if m.GetMessage() != m.GetCode() {
		t.Errorf("message %q diverged from code %q", m.GetMessage(), m.GetCode())
	}
}
