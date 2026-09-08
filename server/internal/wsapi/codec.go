package wsapi

import (
	"errors"
	"fmt"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"google.golang.org/protobuf/proto"
)

// maxFrameBytes caps what a client may send.
//
// The largest legitimate client frame is a Hello carrying a 20-rune nickname,
// well under 200 bytes. 4 KiB leaves room for the protocol to grow while still
// making a memory-exhaustion attempt fail at the socket rather than in the
// decoder.
const maxFrameBytes = 4096

// ErrNotBinary rejects text frames. The protocol is protobuf over binary
// frames only; a text frame means the peer is speaking something else, and
// guessing at it is how a parser becomes an attack surface.
var ErrNotBinary = errors.New("wsapi: expected a binary frame")

// Encode marshals a server message for the wire.
func Encode(m *noituv1.ServerMessage) ([]byte, error) {
	raw, err := proto.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("wsapi: encode: %w", err)
	}
	return raw, nil
}

// Decode parses a client frame.
//
// typ is checked here rather than at the call site so every entry point gets
// the same rejection.
func Decode(typ websocket.MessageType, raw []byte) (*noituv1.ClientMessage, error) {
	if typ != websocket.MessageBinary {
		return nil, ErrNotBinary
	}
	var m noituv1.ClientMessage
	if err := proto.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("wsapi: decode: %w", err)
	}
	return &m, nil
}

// Message-building helpers. Every server message is constructed through one of
// these, so the oneof wrapper is written once per variant instead of at each
// call site.

func welcomeMsg(sessionID, resumeToken, nickname string) *noituv1.ServerMessage {
	return &noituv1.ServerMessage{Payload: &noituv1.ServerMessage_Welcome{Welcome: &noituv1.Welcome{
		SessionId:        sessionID,
		ResumeToken:      resumeToken,
		ProtocolVersion:  ProtocolVersion,
		AcceptedNickname: nickname,
	}}}
}

func moveRejectedMsg(reason noituv1.RejectReason, word string, turnSeq uint32) *noituv1.ServerMessage {
	return &noituv1.ServerMessage{Payload: &noituv1.ServerMessage_MoveRejected{
		MoveRejected: &noituv1.MoveRejected{Reason: reason, Word: word, TurnSeq: turnSeq},
	}}
}

// errorMsg carries a UI key, never prose and never an internal error string.
// The Vietnamese copy lives in the frontend so all wording stays in one place,
// and a raw error would leak server internals to anyone with a socket.
func errorMsg(code string) *noituv1.ServerMessage {
	return &noituv1.ServerMessage{Payload: &noituv1.ServerMessage_Error{
		Error: &noituv1.ServerError{Code: code, Message: code},
	}}
}

func pongMsg(clientTimeMs, serverTimeMs int64) *noituv1.ServerMessage {
	return &noituv1.ServerMessage{Payload: &noituv1.ServerMessage_Pong{
		Pong: &noituv1.Pong{ClientTimeMs: clientTimeMs, ServerTimeMs: serverTimeMs},
	}}
}
