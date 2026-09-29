/**
 * Whether the player is typing in a field other than `own`. Taking focus off
 * a field somebody is mid-sentence in would drop the rest of that sentence
 * into whatever took it, so anything that grabs focus on its own schedule (a
 * turn arriving, a game ending) asks first.
 * @param {Element | null | undefined} [own] - the caller's own field, which
 *   holding focus does not count as typing elsewhere
 * @returns {boolean}
 */
export function typingElsewhere(own) {
	const active = document.activeElement;
	if (!active || active === own) return false;
	return (
		active instanceof HTMLElement &&
		(active.tagName === 'INPUT' || active.tagName === 'TEXTAREA' || active.isContentEditable)
	);
}
