// Not a test file: a place for the `$state` rune, which only compiles in a
// `.svelte.js` module, so a test can change a mounted component's props.

/**
 * @template {Record<string, unknown>} T
 * @param {T} initial
 * @returns {T}
 */
export function reactiveProps(initial) {
	const props = $state(initial);
	return props;
}
