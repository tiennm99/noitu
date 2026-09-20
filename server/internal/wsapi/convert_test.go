package wsapi

import (
	"testing"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/bot"
	"github.com/tiennm99dev/noitu/server/internal/game"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// engineRejectReasons enumerates the engine's constants from its own count
// sentinel.
//
// The sentinel is what makes this honest. Walking until String returns
// "unknown" would be circular — a reason added without a String case would
// shorten the range under test and every assertion below would still pass on
// a value that has no mapping.
func engineRejectReasons() []game.RejectReason {
	out := make([]game.RejectReason, 0, game.NumRejectReasons)
	for i := game.RejectReason(0); i < game.NumRejectReasons; i++ {
		out = append(out, i)
	}
	return out
}

func engineEndReasons() []game.EndReason {
	out := make([]game.EndReason, 0, game.NumEndReasons)
	for i := game.EndReason(0); i < game.NumEndReasons; i++ {
		out = append(out, i)
	}
	return out
}

// TestEveryEngineReasonIsNamed catches the other half of the same problem: a
// constant added inside the block but not to String. Such a value maps and
// logs as "unknown", which is unreadable in exactly the situation the log
// exists for.
func TestEveryEngineReasonIsNamed(t *testing.T) {
	for _, r := range engineRejectReasons() {
		if r.String() == "unknown" {
			t.Errorf("game.RejectReason(%d) has no String case", int(r))
		}
	}
	for _, r := range engineEndReasons() {
		if r.String() == "unknown" {
			t.Errorf("game.EndReason(%d) has no String case", int(r))
		}
	}
}

// TestRejectReasonMappingIsExhaustive fails if the engine gains a rejection
// with no wire counterpart. Only ReasonNone is allowed to land on UNSPECIFIED:
// it means "accepted", so it never travels inside a MoveRejected.
func TestRejectReasonMappingIsExhaustive(t *testing.T) {
	reasons := engineRejectReasons()
	if len(reasons) < 2 {
		t.Fatalf("enumerated %d engine reject reasons, expected the full set", len(reasons))
	}

	seen := make(map[noituv1.RejectReason]game.RejectReason, len(reasons))
	for _, r := range reasons {
		got := RejectReason(r)
		if r == game.ReasonNone {
			if got != noituv1.RejectReason_REJECT_REASON_UNSPECIFIED {
				t.Errorf("ReasonNone should map to UNSPECIFIED, got %v", got)
			}
			continue
		}
		if got == noituv1.RejectReason_REJECT_REASON_UNSPECIFIED {
			t.Errorf("game.RejectReason(%d) %q has no wire mapping", int(r), r)
			continue
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%v is produced by both %q and %q; a wire value must identify one reason", got, prev, r)
		}
		seen[got] = r
	}

	// Every wire value must also be reachable. An unreachable one is a contract
	// the server can never honour, which a client would be right to handle.
	for _, v := range enumValues(noituv1.RejectReason_REJECT_REASON_UNSPECIFIED.Descriptor()) {
		w := noituv1.RejectReason(v)
		if w == noituv1.RejectReason_REJECT_REASON_UNSPECIFIED {
			continue
		}
		if _, ok := seen[w]; !ok {
			t.Errorf("wire value %v is unreachable: no engine reason maps to it", w)
		}
	}
}

// TestEndReasonMappingIsExhaustive mirrors the reject-reason walk.
// GAME_END_REASON_OPPONENT_LEFT is the one deliberate gap: a disconnect is a
// transport event the engine has no concept of, so the room emits it directly.
func TestEndReasonMappingIsExhaustive(t *testing.T) {
	seen := make(map[noituv1.GameEndReason]game.EndReason)
	for _, r := range engineEndReasons() {
		got := EndReason(r)
		if r == game.EndNone {
			if got != noituv1.GameEndReason_GAME_END_REASON_UNSPECIFIED {
				t.Errorf("EndNone should map to UNSPECIFIED, got %v", got)
			}
			continue
		}
		if got == noituv1.GameEndReason_GAME_END_REASON_UNSPECIFIED {
			t.Errorf("game.EndReason(%d) %q has no wire mapping", int(r), r)
			continue
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%v is produced by both %q and %q", got, prev, r)
		}
		seen[got] = r
	}

	transportOnly := map[noituv1.GameEndReason]bool{
		noituv1.GameEndReason_GAME_END_REASON_OPPONENT_LEFT: true,
	}
	for _, v := range enumValues(noituv1.GameEndReason_GAME_END_REASON_UNSPECIFIED.Descriptor()) {
		w := noituv1.GameEndReason(v)
		if w == noituv1.GameEndReason_GAME_END_REASON_UNSPECIFIED || transportOnly[w] {
			continue
		}
		if _, ok := seen[w]; !ok {
			t.Errorf("wire value %v is unreachable: no engine reason maps to it", w)
		}
	}
}

func enginePointKinds() []game.PointKind {
	out := make([]game.PointKind, 0, game.NumPointKinds)
	for i := game.PointKind(0); i < game.NumPointKinds; i++ {
		out = append(out, i)
	}
	return out
}

// TestPointKindMappingIsExhaustive mirrors the reject-reason walk. Only
// PointKindNone is allowed to land on UNSPECIFIED: a PointPart exists only for
// a term that contributed, so no real move ever carries that value.
func TestPointKindMappingIsExhaustive(t *testing.T) {
	kinds := enginePointKinds()
	if len(kinds) < 2 {
		t.Fatalf("enumerated %d engine point kinds, expected the full set", len(kinds))
	}

	seen := make(map[noituv1.PointKind]game.PointKind, len(kinds))
	for _, k := range kinds {
		got := PointKind(k)
		if k == game.PointKindNone {
			if got != noituv1.PointKind_POINT_KIND_UNSPECIFIED {
				t.Errorf("PointKindNone should map to UNSPECIFIED, got %v", got)
			}
			continue
		}
		if got == noituv1.PointKind_POINT_KIND_UNSPECIFIED {
			t.Errorf("game.PointKind(%d) %q has no wire mapping", int(k), k)
			continue
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%v is produced by both %q and %q", got, prev, k)
		}
		seen[got] = k
	}

	for _, v := range enumValues(noituv1.PointKind_POINT_KIND_UNSPECIFIED.Descriptor()) {
		w := noituv1.PointKind(v)
		if w == noituv1.PointKind_POINT_KIND_UNSPECIFIED {
			continue
		}
		if _, ok := seen[w]; !ok {
			t.Errorf("wire value %v is unreachable: no engine point kind maps to it", w)
		}
	}
}

// TestPlayedWordCarriesItsScoreBreakdown guards the invariant the client
// relies on: the parts it draws beside the total must sum to it exactly.
func TestPlayedWordCarriesItsScoreBreakdown(t *testing.T) {
	m := game.Move{
		Player: "p1",
		Word:   "bình yên",
		Points: 25,
		Parts: []game.PointPart{
			{Kind: game.PointKindBase, Value: 10},
			{Kind: game.PointKindSpeed, Value: 15},
		},
	}

	rendered := PlayedWord(m, true, nil)
	if len(rendered.GetParts()) != 2 {
		t.Fatalf("rendered %d parts, want 2", len(rendered.GetParts()))
	}
	sum := uint32(0)
	for _, p := range rendered.GetParts() {
		sum += p.GetValue()
	}
	if sum != rendered.GetPoints() {
		t.Errorf("parts sum to %d, want points %d", sum, rendered.GetPoints())
	}
	if rendered.GetParts()[0].GetKind() != noituv1.PointKind_POINT_KIND_BASE {
		t.Errorf("first part kind = %v, want BASE", rendered.GetParts()[0].GetKind())
	}
	if rendered.GetParts()[1].GetKind() != noituv1.PointKind_POINT_KIND_SPEED {
		t.Errorf("second part kind = %v, want SPEED", rendered.GetParts()[1].GetKind())
	}
}

// TestDifficultyMapping checks each level and, more importantly, that an
// unknown wire difficulty is refused rather than defaulted. Difficulty arrives
// from the client, so silently treating garbage as Easy would let a malformed
// frame choose the opponent.
func TestDifficultyMapping(t *testing.T) {
	want := map[noituv1.Difficulty]bot.Difficulty{
		noituv1.Difficulty_DIFFICULTY_EASY:   bot.Easy,
		noituv1.Difficulty_DIFFICULTY_MEDIUM: bot.Medium,
		noituv1.Difficulty_DIFFICULTY_HARD:   bot.Hard,
	}
	for wire, expect := range want {
		got, ok := Difficulty(wire)
		if !ok || got != expect {
			t.Errorf("Difficulty(%v) = (%v, %v), want (%v, true)", wire, got, ok, expect)
		}
	}

	// Every declared level must be reachable, so adding one to the schema
	// without teaching the server about it fails here rather than at runtime.
	for _, v := range enumValues(noituv1.Difficulty_DIFFICULTY_UNSPECIFIED.Descriptor()) {
		d := noituv1.Difficulty(v)
		if d == noituv1.Difficulty_DIFFICULTY_UNSPECIFIED {
			continue
		}
		if _, ok := want[d]; !ok {
			t.Errorf("wire difficulty %v has no bot strategy", d)
		}
	}

	for _, bad := range []noituv1.Difficulty{
		noituv1.Difficulty_DIFFICULTY_UNSPECIFIED,
		noituv1.Difficulty(99),
	} {
		if _, ok := Difficulty(bad); ok {
			t.Errorf("Difficulty(%v) accepted an unknown wire value", bad)
		}
	}
}

// TestPlayedWordKeepsTypedInput guards the correction-visibility rule: the UI
// has to be able to show that canonicalization changed the player's text, so
// the raw input must survive onto the wire.
func TestPlayedWordKeepsTypedInput(t *testing.T) {
	m := game.Move{Player: "p2", Word: "hòa bình", Typed: "hoà bình", Syllables: 2, Points: 2}

	mine := PlayedWord(m, true, nil)
	if mine.GetWord() != "hòa bình" || mine.GetTyped() != "hoà bình" {
		t.Errorf("canonical/typed pair lost: word=%q typed=%q", mine.GetWord(), mine.GetTyped())
	}
	// The byline in a room of three or four is drawn from this field alone;
	// by_me only says whether it was the recipient.
	if mine.GetPlayerId() != "p2" {
		t.Errorf("player_id lost: got %q", mine.GetPlayerId())
	}
	if !mine.GetByMe() || mine.GetSyllables() != 2 || mine.GetPoints() != 2 {
		t.Errorf("unexpected rendering: %+v", mine)
	}
	if theirs := PlayedWord(m, false, nil); theirs.GetByMe() {
		t.Error("by_me must be false for the opponent's copy of the same move")
	}
}

func enumValues(d protoreflect.EnumDescriptor) []protoreflect.EnumNumber {
	vals := d.Values()
	out := make([]protoreflect.EnumNumber, 0, vals.Len())
	for i := 0; i < vals.Len(); i++ {
		out = append(out, vals.Get(i).Number())
	}
	return out
}
