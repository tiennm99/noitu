// The upstream dictionary is pinned in two places: the Makefile, which builds
// it for a developer, and the Dockerfile, which builds it for the image. They
// have to agree, or the container ships a wordlist nobody tested against.
//
// This lives in the JavaScript suite for no better reason than that it is the
// suite that already reads other files in the repository. It is checking two
// build files, not the frontend.

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

describe('the pinned upstream dictionary', () => {
	const makeUrl = pin(makefile, /DICT_URL\s*:?=\s*(\S+)/, 'DICT_URL in the Makefile');
	const makeSha = pin(makefile, /DICT_SHA256\s*:?=\s*(\S+)/, 'DICT_SHA256 in the Makefile');
	const dockerUrl = pin(dockerfile, /ARG DICT_URL=(\S+)/, 'DICT_URL in the Dockerfile');
	const dockerSha = pin(dockerfile, /ARG DICT_SHA256=(\S+)/, 'DICT_SHA256 in the Dockerfile');

	it('is the same release in both build files', () => {
		expect(dockerUrl).toBe(makeUrl);
	});

	it('is pinned to the same checksum in both build files', () => {
		expect(dockerSha).toBe(makeSha);
	});

	it('is a checksum, not a placeholder', () => {
		// A guard that only compared the two would pass happily if both were
		// blanked, which is the one way this pin can silently stop pinning.
		expect(makeSha).toMatch(/^[0-9a-f]{64}$/);
	});

	it('is the commit the builder stamps into the database', () => {
		// The builder records the upstream commit in the meta table from its
		// own constant. A pin bump that misses it would ship an attribution
		// record naming bytes nobody downloaded, so the three copies must agree.
		const builder = readFileSync(
			fileURLToPath(new URL('../../server/cmd/build-dictionary/merged_list.go', import.meta.url)),
			'utf8'
		);
		const commit = pin(builder, /mergedSourceCommit\s*=\s*"([0-9a-f]{40})"/, 'mergedSourceCommit in the builder');
		expect(makeUrl).toContain(`/${commit}/`);
	});
});
