<script>
	/**
	 * A press-twice control, in place of a native confirm(): resigning,
	 * claiming a dead end and kicking a player all need "are you sure"
	 * without one, because confirm() blocks the main thread and the
	 * countdown ring's frame loop keeps running underneath it — hesitating
	 * over the dialog can cost the very turn the confirmation was meant to
	 * protect.
	 *
	 * The second press is the same control asking again, not a different
	 * one, so this owns the arm timer and disarms itself the moment
	 * `disabled` goes true — an armed button that loses the offer it was
	 * making (the turn moves on, the seat becomes unkickable) must not sit
	 * there waiting for a press that would now mean something else.
	 *
	 * `aria-pressed` carries the armed state to assistive tech. A screen
	 * reader announces a control's accessible name on focus, not on an
	 * in-place mutation of it, so a swapped label alone is silent to a
	 * non-sighted player on the first press; the pressed-state change on the
	 * element they already have focus on is what gets spoken.
	 * @type {{
	 *   label: string,
	 *   confirmLabel: string,
	 *   onconfirm: () => void,
	 *   disabled?: boolean,
	 *   armMs?: number,
	 *   class?: string,
	 *   testid?: string,
	 *   children?: import('svelte').Snippet
	 * }}
	 */
	let {
		label,
		confirmLabel,
		onconfirm,
		disabled = false,
		armMs = 4000,
		class: className = '',
		testid,
		children
	} = $props();

	let armed = $state(false);
	/** @type {ReturnType<typeof setTimeout>} */
	let timer;

	$effect(() => {
		if (disabled) {
			clearTimeout(timer);
			armed = false;
		}
	});

	$effect(() => () => clearTimeout(timer));

	function press() {
		if (armed) {
			clearTimeout(timer);
			armed = false;
			onconfirm();
			return;
		}
		armed = true;
		clearTimeout(timer);
		timer = setTimeout(() => (armed = false), armMs);
	}
</script>

<button
	type="button"
	class={className}
	class:arming={armed}
	{disabled}
	aria-pressed={armed}
	aria-label={armed ? confirmLabel : label}
	data-testid={testid}
	onclick={press}
>
	{#if children}
		{@render children()}
	{:else}
		{armed ? confirmLabel : label}
	{/if}
</button>
