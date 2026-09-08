// The wordlist must never reach the browser. If it did, the dictionary would
// stop being the server's secret and every rule it enforces would become
// advisory — a player could read the valid answers out of their own bundle.
//
// npm test builds before running, so this check always reads a bundle produced
// from the current source rather than whatever was left in build/.

import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, extname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const buildDir = fileURLToPath(new URL('../build', import.meta.url));
const srcDir = fileURLToPath(new URL('../src', import.meta.url));

/**
 * Every file in the built output, recursively.
 *
 * @param {string} dir
 * @returns {string[]}
 */
function walk(dir) {
	return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
		const full = join(dir, entry.name);
		return entry.isDirectory() ? walk(full) : [full];
	});
}

/**
 * Turns `\uXXXX` sequences back into the characters they stand for.
 *
 * A minifier configured to emit ASCII writes Vietnamese as escapes, and a
 * substring search over the raw bytes would then find nothing while the words
 * are plainly there in the shipped file.
 *
 * @param {string} text
 * @returns {string}
 */
function decodeUnicodeEscapes(text) {
	return text.replace(/\\u([0-9a-fA-F]{4})/g, (_, hex) => String.fromCharCode(parseInt(hex, 16)));
}

/**
 * Words that belong to the dictionary and to nothing else in this project.
 * None of them is UI copy, a test fixture, or a placeholder, so any hit is the
 * wordlist leaking rather than a coincidence.
 */
const DICTIONARY_WORDS = [
	'học sinh',
	'sinh viên',
	'bàn ghế',
	'quốc gia',
	'con mèo',
	'giáo viên',
	'thành phố',
	'nhà cửa'
];

/**
 * A budget that binds. The bundle is around 190 KB, so this leaves room for
 * more screens without leaving room for a wordlist: the derived dictionary is
 * several MB, and even a fraction of it would not fit here.
 */
const MAX_TOTAL_BYTES = 400_000;

const files = walk(buildDir);

describe('built bundle', () => {
	it('is newer than the source it was built from', () => {
		// `npm test` builds first, so this should always hold. It is asserted
		// anyway because running vitest directly would otherwise search whatever
		// happened to be left in build/ — passing over code that no longer
		// exists, which is the one way this whole file could be vacuous.
		expect(files.length).toBeGreaterThan(0);
		expect(files.some((f) => f.endsWith('index.html'))).toBe(true);

		const newestBuild = Math.max(...files.map((f) => statSync(f).mtimeMs));
		const newestSource = Math.max(...walk(srcDir).map((f) => statSync(f).mtimeMs));

		expect(
			newestBuild,
			'build/ is older than src/ — run `npm test`, which builds first'
		).toBeGreaterThan(newestSource);
	});

	it('carries no dictionary words', () => {
		/** @type {string[]} */
		const hits = [];
		for (const file of files) {
			const text = decodeUnicodeEscapes(readFileSync(file, 'utf8'));
			for (const word of DICTIONARY_WORDS) {
				if (text.includes(word)) hits.push(`${file}: ${word}`);
			}
		}
		expect(hits).toEqual([]);
	});

	it('ships no database or wordlist file', () => {
		const dataFiles = files.filter((f) =>
			['.db', '.sqlite', '.sqlite3', '.csv', '.tsv'].includes(extname(f))
		);
		expect(dataFiles).toEqual([]);
	});

	it('stays within the size budget that separates code from data', () => {
		const total = files.reduce((sum, f) => sum + statSync(f).size, 0);
		expect(total).toBeLessThan(MAX_TOTAL_BYTES);
	});
});
