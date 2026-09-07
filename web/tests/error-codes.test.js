// ServerError.code is a UI key, so the server decides the vocabulary and this
// file has to speak all of it. The proto schema cannot help here — the codes
// are string literals in the Go transport, not an enum — so the guard reads
// them out of the source that emits them.
//
// Without this, adding a code on the server degrades silently to the generic
// fallback: the player is told "something went wrong" for a situation the
// server described precisely.

import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { errorMessages } from '../src/lib/i18n/vi.js';

const wsapiDir = fileURLToPath(new URL('../../server/internal/wsapi', import.meta.url));

/**
 * Every error-code literal in the transport, excluding its own tests. Both the
 * one-recipient and the broadcast call sites count: a code that only ever goes
 * to both players is no less a code the client has to know.
 */
function serverErrorCodes() {
	const codes = new Set();
	const files = readdirSync(wsapiDir).filter((f) => f.endsWith('.go') && !f.endsWith('_test.go'));

	for (const file of files) {
		const source = readFileSync(join(wsapiDir, file), 'utf8');
		for (const [, code] of source.matchAll(/(?:errorMsg|broadcastError)\("([a-z_]+)"\)/g)) {
			codes.add(code);
		}
	}
	return codes;
}

describe('server error codes', () => {
	const codes = serverErrorCodes();

	it('finds the call sites at all, so an empty match cannot pass as agreement', () => {
		// A rename of errorMsg would otherwise turn this whole file green by
		// finding nothing to check.
		expect(codes.size).toBeGreaterThan(10);
		expect(codes.has('room_not_found')).toBe(true);
	});

	it('has a Vietnamese message for every code the server can send', () => {
		const missing = [...codes].filter((code) => !(code in errorMessages)).sort();
		expect(missing).toEqual([]);
	});

	it('carries no message for a code the server cannot send', () => {
		// Dead copy is a smaller problem than a missing message, but it is still
		// a claim about the server that has stopped being true.
		const stale = Object.keys(errorMessages)
			.filter((code) => !codes.has(code))
			.sort();
		expect(stale).toEqual([]);
	});
});
