<script>
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/**
	 * The banner shown while an opponent is inside their reconnect window.
	 *
	 * The countdown is local because the server sends a duration, not a
	 * deadline: it is a rough "how much longer" for the player watching, and
	 * the server still decides when the seat is actually forfeit.
	 */
	let remainingMs = $state(0);

	const away = $derived(game.state.opponentLeft);

	$effect(() => {
		const left = away;
		if (!left?.canReconnect) {
			remainingMs = 0;
			return;
		}

		const endsAt = Date.now() + left.graceMs;
		remainingMs = left.graceMs;

		const tick = setInterval(() => {
			remainingMs = Math.max(0, endsAt - Date.now());
			if (remainingMs === 0) clearInterval(tick);
		}, 250);
		return () => clearInterval(tick);
	});

	const seconds = $derived(Math.ceil(remainingMs / 1000));
</script>

{#if away}
	<p class="banner" class:gone={!away.canReconnect} role="status" data-testid="opponent-status">
		{#if !away.canReconnect}
			{t.opponentGone}
		{:else if seconds > 0}
			{fill(t.opponentDisconnectedIn, { n: seconds })}
		{:else}
			{t.opponentDisconnected}
		{/if}
	</p>
{/if}

<style>
	.banner {
		margin: 0;
		padding: 10px 12px;
		border-radius: var(--radius-sm);
		background: var(--surface-alt);
		color: var(--warn);
		font-size: 0.9rem;
		text-align: center;
	}

	.banner.gone {
		color: var(--danger);
		background: var(--danger-soft);
	}
</style>
