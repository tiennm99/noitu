package wsapi

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// -update rewrites the committed cross-language fixtures. The JS suite decodes
// the same bytes, so regenerating them is the deliberate act of changing the
// wire contract, not a side effect of running the Go tests.
var update = flag.Bool("update", false, "rewrite the cross-language fixtures in proto/testdata")

const fixtureDir = "../../../proto/testdata"

// clientVariants covers every arm of ClientMessage.payload.
//
// The values are not minimal on purpose: Vietnamese text with diacritics, a
// large turn_seq and a real millisecond timestamp all exercise encodings a
// zero value would skip — UTF-8, varint width, and int64, which the JavaScript
// runtime surfaces as a bigint.
func clientVariants() map[string]*noituv1.ClientMessage {
	return map[string]*noituv1.ClientMessage{
		"client_hello": {Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
			ProtocolVersion: 1,
			ResumeToken:     "r-8f2c",
			Nickname:        "Người chơi ẩn danh",
		}}},
		"client_start_bot_game": {Payload: &noituv1.ClientMessage_StartBotGame{StartBotGame: &noituv1.StartBotGame{
			Difficulty: noituv1.Difficulty_DIFFICULTY_HARD,
		}}},
		"client_create_room": {Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}},
		"client_join_room": {Payload: &noituv1.ClientMessage_JoinRoom{JoinRoom: &noituv1.JoinRoom{
			RoomCode: "K7QX",
		}}},
		"client_submit_word": {Payload: &noituv1.ClientMessage_SubmitWord{SubmitWord: &noituv1.SubmitWord{
			Word:    "hoà bình",
			TurnSeq: 4242,
		}}},
		"client_resign": {Payload: &noituv1.ClientMessage_Resign{Resign: &noituv1.Resign{}}},
		"client_ping": {Payload: &noituv1.ClientMessage_Ping{Ping: &noituv1.Ping{
			ClientTimeMs: 1756998000123,
		}}},
		"client_set_ready": {Payload: &noituv1.ClientMessage_SetReady{SetReady: &noituv1.SetReady{
			Ready: true,
		}}},
		"client_start_game":  {Payload: &noituv1.ClientMessage_StartGame{StartGame: &noituv1.StartGame{}}},
		"client_kick_player": {Payload: &noituv1.ClientMessage_KickPlayer{KickPlayer: &noituv1.KickPlayer{}}},
		"client_leave_room":  {Payload: &noituv1.ClientMessage_LeaveRoom{LeaveRoom: &noituv1.LeaveRoom{}}},
	}
}

// serverVariants covers every arm of ServerMessage.payload.
func serverVariants() map[string]*noituv1.ServerMessage {
	return map[string]*noituv1.ServerMessage{
		"server_welcome": {Payload: &noituv1.ServerMessage_Welcome{Welcome: &noituv1.Welcome{
			SessionId:        "s-1a2b3c",
			ResumeToken:      "r-8f2c",
			ProtocolVersion:  1,
			AcceptedNickname: "Người chơi ẩn danh",
		}}},
		"server_game_started": {Payload: &noituv1.ServerMessage_GameStarted{GameStarted: &noituv1.GameStarted{
			OpeningWord:     "hòa bình",
			CurrentSyllable: "bình",
			MyTurn:          true,
			DeadlineUnixMs:  1756998020000,
			TurnSeq:         1,
			TurnLimitMs:     20000,
		}}},
		"server_turn_update": {Payload: &noituv1.ServerMessage_TurnUpdate{TurnUpdate: &noituv1.TurnUpdate{
			Played: &noituv1.PlayedWord{
				Word:      "bình yên",
				Typed:     "binh yên",
				ByMe:      false,
				Points:    2,
				Syllables: 2,
			},
			CurrentSyllable: "yên",
			MyTurn:          true,
			DeadlineUnixMs:  1756998040000,
			TurnSeq:         2,
			MyScore:         3,
			OpponentScore:   5,
			ChainLength:     2,
		}}},
		"server_move_rejected": {Payload: &noituv1.ServerMessage_MoveRejected{MoveRejected: &noituv1.MoveRejected{
			Reason:  noituv1.RejectReason_REJECT_REASON_WRONG_LINK,
			Word:    "cà phê",
			TurnSeq: 2,
		}}},
		"server_game_over": {Payload: &noituv1.ServerMessage_GameOver{GameOver: &noituv1.GameOver{
			IWon:        false,
			Reason:      noituv1.GameEndReason_GAME_END_REASON_NO_LEGAL_MOVE,
			MyScore:     7,
			ChainLength: 11,
			// A repeated string of Vietnamese words: the one field in the
			// contract whose encoding is neither a scalar nor a submessage.
			Suggestions: []string{"sinh viên", "sinh sôi"},
		}}},
		"server_opponent_left": {Payload: &noituv1.ServerMessage_OpponentLeft{OpponentLeft: &noituv1.OpponentLeft{
			CanReconnect: true,
			GraceMs:      30000,
		}}},
		"server_error": {Payload: &noituv1.ServerMessage_Error{Error: &noituv1.ServerError{
			Code:    "room_not_found",
			Message: "room_not_found",
		}}},
		"server_pong": {Payload: &noituv1.ServerMessage_Pong{Pong: &noituv1.Pong{
			ClientTimeMs: 1756998000123,
			ServerTimeMs: 1756998000456,
		}}},
		// Asymmetric on purpose: equal booleans would not catch the two fields
		// being swapped, which is exactly the mistake that shows one player
		// their opponent's answer as their own.
		"server_room_state": {Payload: &noituv1.ServerMessage_RoomState{RoomState: &noituv1.RoomState{
			RoomCode: "K7QX",
			// An owner looking at a guest who is here, ready, and connected:
			// the one combination in which every boolean is load-bearing.
			IAmOwner:          true,
			CanStart:          true,
			IAmReady:          false,
			OpponentPresent:   true,
			OpponentName:      "Khách mời",
			OpponentReady:     true,
			OpponentConnected: true,
		}}},
	}
}

// TestRoundTripEveryVariant marshals and unmarshals each oneof arm. Equality
// alone is not enough: an empty arm such as CreateRoom encodes to a payload of
// zero bytes, so checking that the case survived is what proves the arm is
// distinguishable on the wire at all.
func TestRoundTripEveryVariant(t *testing.T) {
	for name, msg := range clientVariants() {
		t.Run(name, func(t *testing.T) {
			var got noituv1.ClientMessage
			roundTrip(t, msg, &got)
			if got.GetPayload() == nil {
				t.Fatal("payload case lost in round trip")
			}
		})
	}
	for name, msg := range serverVariants() {
		t.Run(name, func(t *testing.T) {
			var got noituv1.ServerMessage
			roundTrip(t, msg, &got)
			if got.GetPayload() == nil {
				t.Fatal("payload case lost in round trip")
			}
		})
	}
}

// TestVariantTablesCoverEveryOneofArm keeps the tables above honest: adding a
// message to either oneof without adding a case here fails the tests rather
// than shipping an untested arm.
func TestVariantTablesCoverEveryOneofArm(t *testing.T) {
	client := make([]proto.Message, 0, len(clientVariants()))
	for _, m := range clientVariants() {
		client = append(client, m)
	}
	server := make([]proto.Message, 0, len(serverVariants()))
	for _, m := range serverVariants() {
		server = append(server, m)
	}
	assertCoversOneof(t, "ClientMessage", client)
	assertCoversOneof(t, "ServerMessage", server)
}

// TestCrossLanguageFixtures asserts the committed bytes still decode to the
// messages above. The JavaScript suite reads the same files, so the two
// languages are checked against one artifact rather than against each other's
// assumptions.
func TestCrossLanguageFixtures(t *testing.T) {
	if *update {
		writeFixtures(t)
		return
	}

	want := allVariants()
	files := mustGlob(t)
	if len(files) != len(want) {
		t.Errorf("fixture count %d does not match variant count %d; run: go test ./internal/wsapi -update", len(files), len(want))
	}

	for _, f := range files {
		name := stem(f)
		expect, ok := want[name]
		if !ok {
			t.Errorf("stale fixture %s has no matching variant", f)
			continue
		}
		delete(want, name)

		raw, err := os.ReadFile(f)
		if err != nil {
			t.Errorf("read %s: %v", f, err)
			continue
		}
		got := expect.ProtoReflect().New().Interface()
		if err := proto.Unmarshal(raw, got); err != nil {
			t.Errorf("decode %s: %v", f, err)
			continue
		}
		if !proto.Equal(got, expect) {
			t.Errorf("%s decoded to %v, want %v", f, got, expect)
		}
	}
	for name := range want {
		t.Errorf("no fixture for variant %q; run: go test ./internal/wsapi -update", name)
	}
}

func writeFixtures(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	// Remove first, so a renamed variant leaves no orphan behind for the JS
	// suite to decode against a schema that no longer describes it.
	for _, f := range mustGlob(t) {
		if err := os.Remove(f); err != nil {
			t.Fatalf("remove %s: %v", f, err)
		}
	}
	all := allVariants()
	for name, m := range all {
		raw, err := proto.Marshal(m)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(fixtureDir, name+".bin"), raw, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	t.Logf("wrote %d fixtures to %s", len(all), fixtureDir)
}

func allVariants() map[string]proto.Message {
	all := map[string]proto.Message{}
	for name, m := range clientVariants() {
		all[name] = m
	}
	for name, m := range serverVariants() {
		all[name] = m
	}
	return all
}

func roundTrip(t *testing.T, in, out proto.Message) {
	t.Helper()
	raw, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := proto.Unmarshal(raw, out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !proto.Equal(out, in) {
		t.Fatalf("round trip changed the message:\n got %v\nwant %v", out, in)
	}
}

// assertCoversOneof reads which arm each sample actually set, rather than
// trusting a list of names written alongside the table. A hardcoded list is
// the thing most likely to be updated in lockstep with the table and so to
// agree with it while both drift away from the schema.
func assertCoversOneof(t *testing.T, msg string, samples []proto.Message) {
	t.Helper()
	if len(samples) == 0 {
		t.Fatalf("%s has no samples", msg)
	}

	covered := map[protoreflect.Name]bool{}
	for _, s := range samples {
		m := s.ProtoReflect()
		oneof := m.Descriptor().Oneofs().ByName("payload")
		if oneof == nil {
			t.Fatalf("%s has no payload oneof", msg)
		}
		set := m.WhichOneof(oneof)
		if set == nil {
			t.Errorf("%s sample %v sets no payload arm", msg, s)
			continue
		}
		covered[set.Name()] = true
	}

	fields := samples[0].ProtoReflect().Descriptor().Oneofs().ByName("payload").Fields()
	for i := 0; i < fields.Len(); i++ {
		if f := fields.Get(i).Name(); !covered[f] {
			t.Errorf("%s.payload arm %q is not in the variant table", msg, f)
		}
	}
}

func mustGlob(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(fixtureDir, "*.bin"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	sort.Strings(files)
	return files
}

func stem(path string) string {
	base := filepath.Base(path)
	return base[:len(base)-len(filepath.Ext(base))]
}
