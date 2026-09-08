import { create } from '@bufbuild/protobuf';
import {
	ClientMessageSchema,
	CreateRoomSchema,
	HelloSchema,
	JoinRoomSchema,
	KickPlayerSchema,
	LeaveRoomSchema,
	PingSchema,
	ResignSchema,
	SendChatSchema,
	SetReadySchema,
	StartGameSchema,
	StartBotGameSchema,
	SubmitWordSchema
} from '$lib/proto/noitu/v1/game_pb.js';

/**
 * The protocol version this build speaks. The server refuses anything else
 * with `protocol_version_mismatch` rather than failing to decode, so this
 * constant is the client half of that contract.
 */
export const PROTOCOL_VERSION = 2;

/**
 * Thin builders, one per client message. They exist so no other module has to
 * know the shape of the `payload` oneof, and so a schema change breaks in one
 * file instead of across the UI.
 *
 * @param {{ nickname: string, resumeToken?: string }} args
 */
export function hello({ nickname, resumeToken = '' }) {
	return create(ClientMessageSchema, {
		payload: {
			case: 'hello',
			value: create(HelloSchema, {
				protocolVersion: PROTOCOL_VERSION,
				resumeToken,
				nickname
			})
		}
	});
}

/** @param {number} difficulty - a Difficulty enum value */
export function startBotGame(difficulty) {
	return create(ClientMessageSchema, {
		payload: {
			case: 'startBotGame',
			value: create(StartBotGameSchema, { difficulty })
		}
	});
}

export function createRoom() {
	return create(ClientMessageSchema, {
		payload: { case: 'createRoom', value: create(CreateRoomSchema, {}) }
	});
}

/** @param {string} roomCode */
export function joinRoom(roomCode) {
	return create(ClientMessageSchema, {
		payload: { case: 'joinRoom', value: create(JoinRoomSchema, { roomCode }) }
	});
}

/**
 * turnSeq is not decoration: the server refuses a submission tagged with a
 * turn that is no longer current, which is what makes a double-submit or a
 * word racing the timeout visible instead of silently applied.
 *
 * @param {string} word
 * @param {number} turnSeq
 */
export function submitWord(word, turnSeq) {
	return create(ClientMessageSchema, {
		payload: { case: 'submitWord', value: create(SubmitWordSchema, { word, turnSeq }) }
	});
}

/**
 * Declares this player ready for the next game, or takes it back. Only guests
 * have a readiness to declare: the owner's is Start itself.
 *
 * @param {boolean} ready
 */
export function setReady(ready) {
	return create(ClientMessageSchema, {
		payload: { case: 'setReady', value: create(SetReadySchema, { ready }) }
	});
}

/** Begins the game the lobby has agreed on. Refused unless every guest is ready. */
export function startGame() {
	return create(ClientMessageSchema, {
		payload: { case: 'startGame', value: create(StartGameSchema, {}) }
	});
}

/**
 * Frees one named seat. Refused while that player is ready, and refused on the
 * owner's own seat — leaving is what an owner who wants out does.
 *
 * @param {string} playerId
 */
export function kickPlayer(playerId) {
	return create(ClientMessageSchema, {
		payload: { case: 'kickPlayer', value: create(KickPlayerSchema, { playerId }) }
	});
}

/** Gives up a seat without dropping the connection. Refused while ready. */
export function leaveRoom() {
	return create(ClientMessageSchema, {
		payload: { case: 'leaveRoom', value: create(LeaveRoomSchema, {}) }
	});
}

/**
 * One line of chat to the rest of the room. The server sanitizes and caps it, so
 * this sends what was typed and lets the copy that comes back be the truth.
 *
 * @param {string} text
 */
export function sendChat(text) {
	return create(ClientMessageSchema, {
		payload: { case: 'sendChat', value: create(SendChatSchema, { text }) }
	});
}

export function resign() {
	return create(ClientMessageSchema, {
		payload: { case: 'resign', value: create(ResignSchema, {}) }
	});
}

/** @param {number} clientTimeMs */
export function ping(clientTimeMs) {
	return create(ClientMessageSchema, {
		payload: {
			case: 'ping',
			value: create(PingSchema, { clientTimeMs: BigInt(Math.trunc(clientTimeMs)) })
		}
	});
}
