<script>
	import { untrack } from 'svelte';
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/**
	 * What is happening to the other people in the room: who has dropped and
	 * how much longer their seat is held, and who has been knocked out.
	 *
	 * The countdown is local because the server sends a duration, not a
	 * deadline: it is a rough "how much longer" for the players watching, and
	 * the server still decides when a seat is actually forfeit.
	 *
	 * @type {Record<string, number>}
	 */
	let endsAt = $state({});
	let now = $state(Date.now());

	const away = $derived(game.awayPlayers);

	// One deadline per player, opened when they drop and forgotten when they
	// come back. Deliberately not keyed off the room state as a whole: it is
	// broadcast on every change a player can see, and rebuilding the deadlines
	// from it would restart every countdown each time somebody said they were
	// ready.
	$effect(() => {
		const present = away.map((p) => p.playerId);
		untrack(() => {
			/** @type {Record<string, number>} */
			const next = {};
			for (const id of present) next[id] = endsAt[id] ?? Date.now() + game.state.graceMs;

			const before = Object.keys(endsAt);
			if (before.length !== present.length || before.some((id) => !(id in next))) {
				endsAt = next;
			}
		});
	});

	$effect(() => {
		if (away.length === 0) return;
		const tick = setInterval(() => (now = Date.now()), 250);
		return () => clearInterval(tick);
	});

	/** @param {string} playerId */
	function secondsLeft(playerId) {
		return Math.max(0, Math.ceil(((endsAt[playerId] ?? 0) - now) / 1000));
	}
</script>

{#if game.iAmOut}
	<!-- The game carries on without this player, and saying so is the whole
	     difference between being knocked out and being disconnected. -->
	<p class="banner gone" role="status" data-testid="eliminated">{t.youAreOut}</p>
{:else if game.state.lastOut && !game.state.lastOut.isMe && game.state.phase === 'playing'}
	<p class="banner" role="status" data-testid="player-out">
		{fill(t.playerOut, { name: game.state.lastOut.name || t.someone })}
	</p>
{/if}

{#each away as player (player.playerId)}
	{@const seconds = secondsLeft(player.playerId)}
	<p class="banner" role="status" data-testid={`away-${player.playerId}`}>
		{#if seconds > 0}
			{fill(t.playerDisconnectedIn, { name: player.name || t.someone, n: seconds })}
		{:else}
			{fill(t.playerDisconnected, { name: player.name || t.someone })}
		{/if}
	</p>
{/each}

<style>
	.banner {
		margin: 0;
		padding: 10px var(--space-3);
		border-radius: var(--radius-sm);
		background: var(--surface-alt);
		color: var(--warn);
		font-size: var(--text-5);
		text-align: center;
	}

	.banner.gone {
		color: var(--danger);
		background: var(--danger-soft);
	}
</style>
