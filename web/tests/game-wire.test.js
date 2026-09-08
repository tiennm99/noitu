// Cross-language wire check.
//
// These fixtures are binary messages produced by the Go server's own test
// suite (server/internal/wsapi/wire_test.go, regenerated with `-update`).
// Decoding them here proves the two generated clients agree on the actual
// bytes — a JS-only round trip would only prove this runtime is
// self-consistent, which is exactly the failure mode that ships a broken
// client.

import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { PROTOCOL_VERSION } from '../src/lib/ws/messages.js';
import { fromBinary } from '@bufbuild/protobuf';
import {
	ClientMessageSchema,
	GameEndReason,
	RejectReason,
	ServerMessageSchema
} from '../src/lib/proto/noitu/v1/game_pb.js';

const fixtureDir = fileURLToPath(new URL('../../proto/testdata', import.meta.url));

const fixtures = readdirSync(fixtureDir).filter((f) => f.endsWith('.bin'));

/** Decode a fixture by name, choosing the schema from its client_/server_ prefix. */
function decode(name) {
	const bytes = readFileSync(join(fixtureDir, `${name}.bin`));
	const schema = name.startsWith('client_') ? ClientMessageSchema : ServerMessageSchema;
	return fromBinary(schema, bytes);
}

describe('generated wire types', () => {
	it('finds the fixtures the Go suite emits', () => {
		expect(fixtures.length).toBeGreaterThan(0);
	});

	// Every arm must decode to a *named* case. An arm whose payload is empty on
	// the wire (create_room, resign) still has to be distinguishable, so an
	// undefined case here means the oneof tag was lost, not that the message
	// was empty.
	it.each(fixtures)('decodes %s to a known payload case', (file) => {
		const msg = decode(file.replace(/\.bin$/, ''));
		expect(msg.payload.case).toBeTypeOf('string');
	});

	it('preserves Vietnamese diacritics through UTF-8', () => {
		const msg = decode('server_welcome');
		expect(msg.payload.case).toBe('welcome');
		expect(msg.payload.value.acceptedNickname).toBe('Người chơi ẩn danh');
		expect(msg.payload.value.protocolVersion).toBe(PROTOCOL_VERSION);
	});

	// int64 is a bigint in this runtime. Reading it as a Number would silently
	// lose precision on timestamps far smaller than the ones a real deadline
	// carries, so the type itself is the assertion.
	it('reads int64 timestamps as bigint', () => {
		const msg = decode('server_pong');
		expect(msg.payload.case).toBe('pong');
		expect(msg.payload.value.clientTimeMs).toBe(1756998000123n);
		expect(msg.payload.value.serverTimeMs).toBe(1756998000456n);
	});

	it('agrees with Go on enum numbering', () => {
		const rejected = decode('server_move_rejected');
		expect(rejected.payload.case).toBe('moveRejected');
		expect(rejected.payload.value.reason).toBe(RejectReason.WRONG_LINK);
		expect(rejected.payload.value.word).toBe('cà phê');

		const over = decode('server_game_over');
		expect(over.payload.case).toBe('gameOver');
		expect(over.payload.value.reason).toBe(GameEndReason.NO_LEGAL_MOVE);
		expect(over.payload.value.iWon).toBe(false);
		// The final table, in the order the server ranked it.
		expect(over.payload.value.standings.map((/** @type {any} */ p) => p.rank)).toEqual([1, 2, 3]);

		const out = decode('server_player_eliminated');
		// The only repeated string in the contract, and the one the losing
		// player's screen is built from.
		expect(out.payload.value.suggestions).toEqual(['sinh viên', 'sinh sôi']);

		const chat = decode('server_chat_message');
		expect(chat.payload.case).toBe('chatMessage');
		expect(chat.payload.value.text).toBe('Tiếng “sinh” khó nối lắm.');
		// int64, which this runtime hands over as a bigint. Anything that
		// forgets to convert it fails here rather than as NaN on a screen.
		expect(typeof chat.payload.value.sentUnixMs).toBe('bigint');

		const history = decode('server_chat_history');
		expect(history.payload.case).toBe('chatHistory');
		const messages = history.payload.value.messages;
		expect(messages).toHaveLength(3);
		expect(messages[0].fromMe).toBe(true);
		expect(messages[1].fromMe).toBe(false);
		// An author who has left the room carries no name, which is how the
		// client knows to label the line rather than attribute it.
		expect(messages[2].author).toBe('');
	});

	// The canonical/typed pair is what lets the UI show that the server
	// corrected a spelling instead of appearing to rewrite the player's text.
	it('carries both the canonical word and the raw input', () => {
		const msg = decode('server_turn_update');
		expect(msg.payload.case).toBe('turnUpdate');
		const turn = msg.payload.value;
		expect(turn.played.word).toBe('bình yên');
		expect(turn.played.typed).toBe('binh yên');
		expect(turn.played.byMe).toBe(false);
		expect(turn.currentSyllable).toBe('yên');
		expect(turn.deadlineUnixMs).toBe(1756998040000n);
		expect(turn.turnSeq).toBe(2);
	});

	// The label and the definition travel as two fields, so the client decides
	// how a label is shown and never has to parse it back out of the text.
	it('carries a word\'s senses on the opening and on each move', () => {
		const start = decode('server_game_started');
		expect(start.payload.value.openingMeanings).toEqual([
			expect.objectContaining({ pos: 'danh từ', gloss: 'Tình trạng không có chiến tranh.' })
		]);

		const turn = decode('server_turn_update');
		const senses = turn.payload.value.played.meanings;
		expect(senses).toHaveLength(2);
		expect(senses[0].pos).toBe('tính từ');
		expect(senses[1].pos).toBe('');
		expect(senses[1].gloss).toBe('Xem bình an.');
	});

	it('decodes an empty client arm without losing its tag', () => {
		const msg = decode('client_create_room');
		expect(msg.payload.case).toBe('createRoom');
	});
});
