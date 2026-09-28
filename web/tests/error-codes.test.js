// ServerError.code is a UI key, so the server decides the vocabulary and this
// file has to speak all of it. The proto schema cannot help here — the codes
// are strings in the Go transport, not an enum — so the guard reads them out
// of the one file that declares them, server/internal/wsapi/errcodes.go.
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
const registry = 'errcodes.go';

/** Every code declared in the registry, as its wire string. */
function serverErrorCodes() {
	const source = readFileSync(join(wsapiDir, registry), 'utf8');
	return new Set([...source.matchAll(/errCode = "([a-z_]+)"/g)].map(([, code]) => code));
}

/**
 * Call sites that hand an emitter a string literal instead of a registry
 * constant. Go converts an untyped literal to errCode silently, so this is
 * what keeps a new code from bypassing the list the rest of this file checks.
 */
function literalCallSites() {
	const files = readdirSync(wsapiDir).filter(
		(f) => f.endsWith('.go') && !f.endsWith('_test.go') && f !== registry
	);
	return files.flatMap((file) =>
		[
			...readFileSync(join(wsapiDir, file), 'utf8').matchAll(
				/\b(?:errorMsg|broadcastError|toRoom)\([^)]*"/g
			)
		].map(([call]) => `${file}: ${call}`)
	);
}

describe('server error codes', () => {
	const codes = serverErrorCodes();

	it('finds the codes at all, so an empty match cannot pass as agreement', () => {
		// A renamed type or moved registry would otherwise turn this whole file
		// green by finding nothing to check.
		expect(codes.size).toBeGreaterThan(10);
		expect(codes.has('room_not_found')).toBe(true);
	});

	it('declares every code in the registry, never inline at a call site', () => {
		expect(literalCallSites()).toEqual([]);
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
