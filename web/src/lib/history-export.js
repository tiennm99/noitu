import { t } from '$lib/i18n/vi.js';

/**
 * Turns a finished game into a plain-text transcript the player can keep.
 *
 * The formatting lives here rather than in the panel so it can be tested
 * without a DOM, and so the only thing the component owns is the click.
 */

/** @param {number} n */
function pad(n) {
	return String(n).padStart(2, '0');
}

/**
 * Local time, because the file is for the person who played the game and not
 * for a machine in another timezone.
 * @param {Date} at
 * @returns {string}
 */
function stamp(at) {
	return `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())} ${pad(at.getHours())}:${pad(at.getMinutes())}`;
}

/**
 * @param {Date} at
 * @returns {string} a filename safe on every platform, with no colons
 */
export function historyFilename(at = new Date()) {
	const d = `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}`;
	return `noi-tu-${d}-${pad(at.getHours())}${pad(at.getMinutes())}.txt`;
}

/**
 * The chain stays in playing order here. It is a sequence of links, and read
 * on its own a transcript only makes sense from the opening word forward —
 * the newest-first ordering is a reading aid for the live board, not the
 * shape of the game.
 *
 * `nameOf` resolves a seat id to the name that seat was playing under. It is
 * passed in rather than read from the store so the transcript can be built and
 * tested without one.
 *
 * A player who rejoined mid-game only holds the words since they came back,
 * because the server's replay carries the opening word and the latest move
 * and nothing between. The result's chain length, which counts the opening
 * word, is the authority for numbering: words are numbered back from it, and
 * a `…` line marks the stretch that is missing.
 * @param {object} args
 * @param {import('$lib/stores/game.svelte.js').ChainEntry[]} args.chain
 * @param {{ iWon: boolean, myScore: number, chainLength: number } | null} [args.result]
 * @param {(playerId: string) => string} [args.nameOf]
 * @param {Date} [args.at]
 * @returns {string}
 */
export function chainToText({ chain, result = null, nameOf = () => '', at = new Date() }) {
	const lines = [`${t.appName} — ${stamp(at)}`];

	if (result) {
		lines.push(result.iWon ? t.won : t.lost);
		lines.push(
			`${t.finalScore}: ${result.myScore} · ${t.chainLength}: ${result.chainLength}`
		);
	}

	lines.push('');

	const total = Math.max(result?.chainLength ?? 0, chain.length);
	const missing = total > chain.length;

	chain.forEach((entry, index) => {
		// The opening word is the first word of any chain, held in full or not.
		const position = entry.opening ? 1 : total - (chain.length - 1 - index);
		const number = `${position}. ${entry.word}`;
		if (entry.opening) {
			lines.push(`${number} (${t.exportOpening})`);
			if (missing) lines.push(t.exportGap);
			return;
		}
		// A four-way chain has to say which of the others played a word, not
		// merely that it was not this player's.
		const who = entry.byMe ? t.you : nameOf(entry.playerId) || t.opponent;
		const points = entry.points > 0 ? ` +${entry.points}` : '';
		lines.push(`${number} — ${who}${points}`);
	});

	return `${lines.join('\n')}\n`;
}

/**
 * Hands the text to the browser as a download.
 * @param {string} filename
 * @param {string} text
 */
export function downloadText(filename, text) {
	const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }));
	const link = document.createElement('a');
	link.href = url;
	link.download = filename;
	link.click();
	// Revoked on the next tick: some browsers read the blob after the click
	// returns, and freeing it synchronously cancels the download.
	setTimeout(() => URL.revokeObjectURL(url), 0);
}
