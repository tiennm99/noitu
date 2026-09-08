// The upstream dictionary URL lives in three places: the Makefile, which
// builds it for a developer, the Dockerfile, which builds it for the image, and
// the builder, which stamps it into the database. They have to agree, or the
// container ships a wordlist nobody tested against. The docs that quote the
// URL are held to the same copy.
//
// This lives in the JavaScript suite for no better reason than that it is the
// suite that already reads other files in the repository. It is checking build
// files and docs, not the frontend.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const makefile = readFileSync(fileURLToPath(new URL('../../Makefile', import.meta.url)), 'utf8');
const dockerfile = readFileSync(fileURLToPath(new URL('../../Dockerfile', import.meta.url)), 'utf8');

/**
 * @param {string} source
 * @param {RegExp} pattern
 * @param {string} what
 */
function pin(source, pattern, what) {
	const match = source.match(pattern);
	expect(match, `${what} not found`).not.toBeNull();
	return match?.[1].trim();
}

describe('the upstream dictionary export', () => {
	const makeUrl = pin(makefile, /DICT_URL\s*:?=\s*(\S+)/, 'DICT_URL in the Makefile');
	const dockerUrl = pin(dockerfile, /ARG DICT_URL=(\S+)/, 'DICT_URL in the Dockerfile');

	it('is the same file in both build files', () => {
		expect(dockerUrl).toBe(makeUrl);
	});

	it('is the Vietnamese-language file of the Vietnamese Wiktionary edition', () => {
		// The path carries a space, so it must stay percent-encoded or make and
		// sh will split it; and it must be the vi edition, not the English one.
		expect(makeUrl).toMatch(/^https:\/\/kaikki\.org\/viwiktionary\/Ti%E1%BA%BFng%20Vi%E1%BB%87t\/[^\s/]+\.jsonl$/);
	});

	it('is the URL the builder stamps into the database', () => {
		// The builder records the source URL in the meta table from its own
		// constant. The three copies must agree or the attribution record names
		// a file nobody downloaded.
		const builder = readFileSync(
			fileURLToPath(new URL('../../server/cmd/build-dictionary/kaikki_list.go', import.meta.url)),
			'utf8'
		);
		const builderUrl = pin(builder, /kaikkiSourceURL\s*=\s*"([^"]+)"/, 'kaikkiSourceURL in the builder');
		expect(builderUrl).toBe(makeUrl);
	});

	it('is the URL the docs quote', () => {
		for (const rel of ['../../README.md', '../../data/ATTRIBUTION.md']) {
			const doc = readFileSync(fileURLToPath(new URL(rel, import.meta.url)), 'utf8');
			expect(doc, `${rel} does not quote DICT_URL`).toContain(makeUrl);
		}
	});
});
