import { create } from '@bufbuild/protobuf';
import {
	ClientMessageSchema,
	CreateRoomSchema,
	HelloSchema,
	JoinRoomSchema,
	PingSchema,
	ResignSchema,
	StartBotGameSchema,
	SubmitWordSchema
} from '$lib/proto/noitu/v1/game_pb.js';

/**
 * The protocol version this build speaks. The server refuses anything else
 * with `protocol_version_mismatch` rather than failing to decode, so this
 * constant is the client half of that contract.
 */
export const PROTOCOL_VERSION = 1;

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
