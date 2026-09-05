<script>
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/** @type {{ onaccept: () => void }} */
	let { onaccept } = $props();

	const offer = $derived(game.state.rematch);

	// Counted down locally from the duration the server sent. The server owns
	// the real expiry and closes the room when it passes; this only stops the
	// prompt from claiming time that has already gone.
	let remainingMs = $state(0);

	$effect(() => {
		const current = offer;
		if (!current) {
			remainingMs = 0;
			return;
		}

		const endsAt = Date.now() + current.expiresInMs;
		remainingMs = current.expiresInMs;

		const tick = setInterval(() => {
			remainingMs = Math.max(0, endsAt - Date.now());
			if (remainingMs === 0) clearInterval(tick);
		}, 250);
		return () => clearInterval(tick);
	});

	const seconds = $derived(Math.ceil(remainingMs / 1000));
	const expired = $derived(!!offer && remainingMs === 0);
</script>

{#if offer}
	<div class="prompt" data-testid="rematch-prompt">
		{#if expired}
			<p class="line">{t.rematchExpired}</p>
		{:else if offer.iAccepted}
			<p class="line">{fill(t.rematchWaiting, { n: seconds })}</p>
		{:else}
			{#if offer.opponentAccepted}
				<p class="line ready">{fill(t.rematchOpponentReady, { n: seconds })}</p>
			{:else}
				<p class="line">{t.rematchAsk}</p>
			{/if}
			<button type="button" onclick={onaccept} data-testid="rematch-accept">
				{t.rematchYes}
			</button>
		{/if}
	</div>
{/if}

<style>
	.prompt {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 10px;
		padding: 14px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface-alt);
	}

	.line {
		margin: 0;
		color: var(--text-muted);
		font-size: 0.9rem;
		text-align: center;
	}

	.line.ready {
		color: var(--accent);
		font-weight: 600;
	}

	button {
		padding: 10px 20px;
		border: 0;
		border-radius: var(--radius-sm);
		background: var(--accent);
		color: var(--accent-text);
		font-weight: 600;
	}
</style>
