<script>
	import { t } from '$lib/i18n/vi.js';

	/**
	 * One line of news the player has to see, announced as it appears: a
	 * refusal from the server, a request that has stalled, a name still
	 * missing. The board, the lobby and the join form all draw it the same
	 * way, so a refusal reads as a refusal wherever it lands.
	 *
	 * `tone` is `error` for something that went wrong and `notice` for
	 * something the player has to do or know. `ondismiss`, when given, adds
	 * the close button — for news that stays true until somebody acts on it,
	 * the caller omits it.
	 * @type {{
	 *   tone?: 'error' | 'notice',
	 *   testid?: string,
	 *   ondismiss?: () => void,
	 *   children: import('svelte').Snippet
	 * }}
	 */
	let { tone = 'error', testid, ondismiss, children } = $props();
</script>

<p class="alert {tone}" role="alert" data-testid={testid}>
	{@render children()}
	{#if ondismiss}
		<button type="button" class="icon-button" onclick={ondismiss} aria-label={t.dismiss}>×</button>
	{/if}
</p>

<style>
	.alert {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		margin: 0;
		padding: var(--space-3);
		border-radius: var(--radius-sm);
		font-size: var(--text-2);
	}

	.error {
		background: var(--danger-soft);
		color: var(--danger);
	}

	.notice {
		background: var(--surface-alt);
		color: var(--warn);
	}
</style>
