<script>
	import RoomCodePanel from '$lib/components/RoomCodePanel.svelte';
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/**
	 * The room between games. Everything it renders comes from the last
	 * RoomState the server sent, and every button is a request the server is
	 * free to refuse — which is why none of them predict the answer by
	 * changing the screen first.
	 *
	 * `compact` drops the room code panel, for the lobby that appears under a
	 * finished game's result rather than as the whole screen.
	 *
	 * @type {{
	 *   compact?: boolean,
	 *   onready: (ready: boolean) => void,
	 *   onstart: () => void,
	 *   onkick: (playerId: string) => void,
	 *   onleave: () => void
	 * }}
	 */
	let { compact = false, onready, onstart, onkick, onleave } = $props();

	const s = $derived(game.state);
	// The seats nobody is in yet, drawn so a room that is waiting on people
	// looks like one rather than like a room that is simply small.
	const empties = $derived(Array.from({ length: game.freeSeats }, (_, i) => i));
	const shortHanded = $derived(s.roomPlayers.length < s.minPlayers);
</script>

<section class="lobby" aria-label={t.lobbyTitle}>
	{#if !compact}
		<RoomCodePanel code={s.roomCode} />
	{/if}

	<p class="count" data-testid="player-count">
		{fill(t.playerCount, { n: s.roomPlayers.length, max: s.maxPlayers })}
	</p>

	<ul class="seats">
		{#each s.roomPlayers as player (player.playerId)}
			<li class="seat" class:ready={player.isOwner || player.ready} class:me={player.isMe}>
				<span class="name">{player.isMe ? s.nickname || t.you : player.name}</span>
				<span class="role">{player.isOwner ? t.owner : t.guest}</span>

				<span class="right">
					<!-- The series, not this game: how many games this seat has
					     taken since the room opened. Shown from zero, so the
					     tally is a thing the room has rather than something
					     that appears once somebody wins. -->
					<span class="wins" data-testid={`wins-${player.playerId}`}>
						{t.winsLabel} <strong>{player.wins}</strong>
					</span>

					{#if !player.connected}
						<span class="state offline">{t.offline}</span>
					{:else if !player.isOwner}
						<!-- The owner has no readiness to show: starting is the statement. -->
						<span class="state" data-testid={player.isMe ? 'my-ready' : `ready-${player.playerId}`}>
							{player.ready ? t.isReady : t.notReady}
						</span>
					{/if}

					<!-- Only the owner sees these, and never on their own row: leaving
					     is what an owner who wants out does, and it hands the room on. -->
					{#if game.isOwner && !player.isMe}
						<button
							type="button"
							class="kick"
							disabled={player.ready}
							aria-label={t.kickPlayer}
							data-testid={`kick-${player.playerId}`}
							onclick={() => onkick(player.playerId)}
						>
							×
						</button>
					{/if}
				</span>
			</li>
		{/each}

		{#each empties as index (index)}
			<li class="seat empty">
				<span class="name muted">{t.emptySeat}</span>
			</li>
		{/each}
	</ul>

	<p class="hint">
		{#if game.isOwner && shortHanded}
			{fill(t.ownerNeedsMore, { n: s.minPlayers })}
		{:else if game.isOwner}
			{t.ownerStartsHint}
		{:else if game.isReady}
			{t.waitingForStart}
		{:else}
			{t.guestReadyHint}
		{/if}
	</p>

	<div class="actions">
		{#if game.isOwner}
			<button
				type="button"
				class="primary"
				disabled={!s.canStart}
				data-testid="start-game"
				onclick={onstart}
			>
				{t.startGame}
			</button>
		{:else}
			<button
				type="button"
				class="primary"
				class:on={game.isReady}
				data-testid="ready"
				onclick={() => onready(!game.isReady)}
			>
				{game.isReady ? t.unready : t.ready}
			</button>
		{/if}
	</div>

	<!-- Disabled rather than hidden while ready: the rule is worth seeing, and
	     a button that vanishes reads as a bug. -->
	<button type="button" class="leave" disabled={game.isReady} onclick={onleave}>
		{t.leaveRoom}
	</button>
	{#if game.isReady}
		<p class="note">{t.unreadyToLeave}</p>
	{/if}
</section>

<style>
	.lobby {
		display: flex;
		flex-direction: column;
		align-items: stretch;
		gap: 14px;
	}

	.count {
		margin: 0;
		color: var(--text-muted);
		font-size: 0.85rem;
		font-weight: 600;
	}

	.seats {
		display: flex;
		flex-direction: column;
		gap: 8px;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.seat {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 10px;
		padding: 12px 14px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
	}

	.seat.ready {
		border-color: var(--accent);
		background: var(--accent-soft);
	}

	.seat.empty {
		border-style: dashed;
		background: var(--surface-alt);
	}

	.name {
		font-weight: 600;
	}

	.name.muted {
		color: var(--text-muted);
		font-weight: 400;
	}

	.role {
		padding: 1px 8px;
		border-radius: 999px;
		background: var(--surface-alt);
		color: var(--text-muted);
		font-size: 0.75rem;
	}

	/* One right-hand group, so a row keeps its shape whether or not it has a
	   readiness to show and whether or not the reader may kick it. */
	.right {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		margin-left: auto;
	}

	.wins {
		color: var(--text-muted);
		font-size: 0.75rem;
		white-space: nowrap;
	}

	.wins strong {
		color: var(--text);
		font-size: 0.9rem;
		font-variant-numeric: tabular-nums;
	}

	.state {
		color: var(--text-muted);
		font-size: 0.8rem;
	}

	.state.offline {
		color: var(--danger);
	}

	.kick {
		width: 26px;
		height: 26px;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: 999px;
		background: transparent;
		color: var(--text-muted);
		font-size: 1rem;
		line-height: 1;
	}

	.kick:disabled {
		opacity: 0.35;
	}

	.hint {
		margin: 0;
		color: var(--text-muted);
		font-size: 0.9rem;
		text-align: center;
	}

	.actions {
		display: flex;
		gap: 8px;
	}

	.actions button {
		flex: 1;
		padding: 14px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
		font-weight: 600;
	}

	.actions .primary {
		border-color: transparent;
		background: var(--accent);
		color: var(--accent-text);
	}

	.actions .primary.on {
		background: var(--surface-alt);
		color: var(--text-muted);
	}

	.actions button:disabled {
		background: var(--surface-alt);
		border-color: var(--border);
		color: var(--text-muted);
	}

	.leave {
		align-self: center;
		padding: 8px 16px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--text-muted);
		font-size: 0.85rem;
	}

	.leave:disabled {
		opacity: 0.5;
	}

	.note {
		margin: 0;
		color: var(--text-muted);
		font-size: 0.8rem;
		text-align: center;
	}
</style>
