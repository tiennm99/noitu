/**
 * Scrolling that honours the reduced-motion setting.
 *
 * The CSS escape hatch in app.css cannot reach this: an explicit `behavior`
 * passed to scrollTo beats any `scroll-behavior` rule, so a smooth scroll asked
 * for in script animates however the reader has configured their system. The
 * chain scrolls on every single move, which makes it the worst offender.
 */

/** @returns {boolean} */
export function prefersReducedMotion() {
	return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
}

/** @returns {ScrollBehavior} */
export function scrollBehavior() {
	return prefersReducedMotion() ? 'auto' : 'smooth';
}
