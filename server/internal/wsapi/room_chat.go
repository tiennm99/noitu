package wsapi

import (
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/game"
)

// The room's own conversation, independent of whatever game is or is not
// running in it.

// chatEntry is one line of the room's conversation.
type chatEntry struct {
	// seq is this message's place in the room's whole conversation, compared
	// against a seat's chatFrom to decide what that player may be replayed.
	seq uint64
	// author and name are cleared together when the seat is vacated: the words
	// stay, the attribution does not. Keeping the name would let the next
	// person to request that nickname inherit a stranger's messages, since
	// distinguish only compares against the seat that is currently occupied.
	author game.PlayerID
	name   string
	text   string
	at     time.Time
}

// handleChat delivers one line of text to everybody in the room.
func (r *room) handleChat(m chatInput) {
	// The seat, not the claimed id. A connection the room has already retired
	// - kicked, or replaced by a reconnect - can still have a frame in flight,
	// and by the time the room drains it that seat may belong to somebody else.
	if !r.occupies(m.sess, m.player) {
		m.sess.send(errorMsg("not_your_seat"))
		return
	}
	// A bot room has no conversation. Checked here rather than in the session,
	// because r.strategy is room-goroutine state.
	if r.strategy != nil {
		m.sess.send(errorMsg("not_in_a_room"))
		return
	}

	text := sanitizeText(m.text, maxChatRunes, maxChatMarks)
	// Nothing usable survived. There is no message to refuse and nobody to
	// tell: the client will not enable its send button for input that reduces
	// to this, so anything reaching here typed nothing.
	if text == "" {
		return
	}

	from := r.seatOf(m.player)
	r.chatSeq++
	entry := chatEntry{
		seq:    r.chatSeq,
		author: from.id,
		name:   from.nickname,
		text:   text,
		at:     time.Now(),
	}
	r.chat = append(r.chat, entry)
	if len(r.chat) > chatHistoryLimit {
		r.chat = r.chat[len(r.chat)-chatHistoryLimit:]
	}
	metrics.chatLines.Add(1)

	for _, s := range r.seats {
		if s == nil || s.sess == nil {
			continue
		}
		// Best effort: a chat frame is dropped rather than allowed to close a
		// session whose outbox is full. Losing a line is recoverable - the
		// next replay carries it - and closing a session costs its owner the
		// game.
		s.sess.trySend(chatMessageFor(entry, s.id))
	}
}

// sendChatHistory replays one seat's slice of the conversation.
//
// Scoped by the seat's chatFrom: a player is shown what was said while they
// were sitting there and nothing else. Sent from the handler, so it reaches the
// client before that input's RoomState - the client must not depend on the
// order, and does not, because a history replaces its panel wholesale.
func (r *room) sendChatHistory(s *seat) {
	if s == nil || s.sess == nil || r.strategy != nil {
		return
	}

	messages := make([]*noituv1.ChatMessage, 0, len(r.chat))
	for _, entry := range r.chat {
		if entry.seq <= s.chatFrom {
			continue
		}
		messages = append(messages, chatMessageFor(entry, s.id).GetChatMessage())
	}

	// send, not trySend: this is the frame that corrects a client's whole
	// panel, including the empty one that clears a conversation carried in
	// from another room. A dropped line recovers on the next replay; a dropped
	// replay has nothing behind it.
	s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_ChatHistory{
		ChatHistory: &noituv1.ChatHistory{Messages: messages},
	}})
}

// chatMessageFor renders one entry from one seat's point of view.
//
// An entry whose author has been cleared belongs to nobody: it is from_me for
// neither player and carries no name, so the seat's next occupant is not shown
// a stranger's words as their own and the player who stayed cannot have them
// reattributed to whoever arrives next.
func chatMessageFor(entry chatEntry, id game.PlayerID) *noituv1.ServerMessage {
	return &noituv1.ServerMessage{Payload: &noituv1.ServerMessage_ChatMessage{
		ChatMessage: &noituv1.ChatMessage{
			FromMe: entry.author != "" && entry.author == id,
			// Empty together with the name for a vacated seat: a line nobody
			// owns must not be coloured as somebody's either.
			PlayerId:   string(entry.author),
			Author:     entry.name,
			Text:       entry.text,
			SentUnixMs: entry.at.UnixMilli(),
		},
	}}
}
