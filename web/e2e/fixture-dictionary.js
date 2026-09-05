import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

/**
 * The same word list the server is playing against, read so a test can answer
 * whatever syllable it is shown.
 *
 * Scripting a fixed sequence of words would not survive the bot choosing a
 * different reply, and the bot is allowed to. Reading the graph instead lets a
 * test assert what matters — that a legal word is accepted and an illegal one
 * is refused — without depending on which legal word the opponent picked.
 */
const listPath = fileURLToPath(new URL('../../testdata/fixture-words.txt', import.meta.url));

const words = readFileSync(listPath, 'utf8')
	.split('\n')
	.map((line) => line.trim())
	.filter((line) => line && !line.startsWith('#'));

/** @type {Map<string, string[]>} */
const byFirstSyllable = new Map();
for (const word of words) {
	const first = word.split(/\s+/)[0];
	const list = byFirstSyllable.get(first) ?? [];
	list.push(word);
	byFirstSyllable.set(first, list);
}

/**
 * A legal continuation of `syllable` that has not been played yet.
 *
 * @param {string} syllable
 * @param {Set<string>} used
 * @returns {string | undefined}
 */
export function continuation(syllable, used) {
	return (byFirstSyllable.get(syllable) ?? []).find((word) => !used.has(word));
}

/**
 * A word that is not in the dictionary at all. Vietnamese-shaped so it passes
 * the syllable-count rule and is refused for the reason the test is about.
 */
export const UNKNOWN_WORD = 'xoăn xoét';

/** A single-syllable input, which the rules refuse before any lookup. */
export const ONE_SYLLABLE_WORD = 'sinh';

/**
 * The syllable every game opens on, and how many words continue it.
 *
 * The server will only open on a word whose last syllable has at least 20
 * continuations (minOpeningOutDegree in room.go). Exactly one syllable in this
 * fixture clears that, which is what makes a scripted game deterministic — and
 * what makes deleting a few words from the list break every browser test with
 * an unhelpful "game_start_failed". Asserting it here turns that into a
 * sentence a reader can act on.
 */
export const HUB_SYLLABLE = 'sinh';
const MIN_OPENING_OUT_DEGREE = 20;

const hubContinuations = (byFirstSyllable.get(HUB_SYLLABLE) ?? []).length;
if (hubContinuations < MIN_OPENING_OUT_DEGREE) {
	throw new Error(
		`testdata/fixture-words.txt has ${hubContinuations} words starting with "${HUB_SYLLABLE}"; ` +
			`the server needs ${MIN_OPENING_OUT_DEGREE} before it will open a game on that syllable`
	);
}

export { words };
