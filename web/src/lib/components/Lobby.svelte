<script>
	import ConnectionBadge from '$lib/components/ConnectionBadge.svelte';
	import PlayerStatus from '$lib/components/PlayerStatus.svelte';
	import RoomCodePanel from '$lib/components/RoomCodePanel.svelte';
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';
	import { Status, connection } from '$lib/ws/connection.svelte.js';

	/**
	 * The room between games. Everything it renders comes from the last
	 * RoomState the server sent, and every button is a request the server is
	 * free to refuse — which is why none of them predict the answer by
	 * changing the screen first.
	 *
	 * `compact` is the lobby that appears under a finished game's result rather
	 * than as the whole screen: the room code shrinks to one line and the board
	 * above already carries the connection state and the away banners.
	 *
	 * The callbacks report whether the request actually reached the server. A
	 * socket that has just dropped answers `false`, and a button that silently
	 * did nothing is the fastest way to make a room look dead.
	 *
	 * @type {{
	 *   compact?: boolean,
	 *   onready: (ready: boolean) => boolean,
	 *   onstart: () => boolean,
	 *   onkick: (playerId: string) => boolean,
	 *   onleave: () => void
	 * }}
	 */
	let { compact = false, onready, onstart, onkick, onleave } = $props();

	/** How long an armed kick waits before it goes back to being safe. */
	const ARM_MS = 4000;

	const s = $derived(game.state);
	// The seats nobody is in yet, drawn so a room that is waiting on people
	// looks like one rather than like a room that is simply small.
	// Not under a finished game: there the count line above says 3/4 already,
	// and three empty rows between the result and the button that starts the
	// next game is what pushed that button off a 1080p screen.
	const empties = $derived(
		compact ? [] : Array.from({ length: game.freeSeats }, (_, i) => i)
	);
	const shortHanded = $derived(s.roomPlayers.length < s.minPlayers);
	const offline = $derived(connection.status !== Status.OPEN);
	// The owner is who everybody else is waiting on, so the hint has to stop
	// telling a ready guest to keep waiting once the owner has dropped.
	const ownerAway = $derived(
		s.roomPlayers.some((/** @type {{ isOwner: boolean, connected: boolean }} */ p) => {
			return p.isOwner && !p.connected;
		})
	);

	/** The seat whose kick button is armed, if any. */
	let armedKick = $state('');
	/** @type {any} */
	let armTimer;
	// Set when a request could not go out at all, which is a different thing
	// from the server refusing it — that arrives as game.state.error.
	let unsent = $state(false);

	/** @param {boolean} sent */
	function report(sent) {
		unsent = !sent;
		return sent;
	}

	/** @param {string} playerId */
	function armOrKick(playerId) {
		if (armedKick === playerId) {
			clearTimeout(armTimer);
			armedKick = '';
			report(onkick(playerId));
			return;
		}
		armedKick = playerId;
		clearTimeout(armTimer);
		armTimer = setTimeout(() => (armedKick = ''), ARM_MS);
	}

	$effect(() => () => clearTimeout(armTimer));
</script>

<section class="lobby" class:compact aria-label={t.lobbyTitle}>
	<!-- The board carries these in the compact case, and two connection badges
	     on one screen say nothing the first one did not. -->
	{#if !compact}
		<div class="top">
			<ConnectionBadge />
		</div>
		<RoomCodePanel code={s.roomCode} />
	{:else}
		<RoomCodePanel code={s.roomCode} compact />
	{/if}

	<p class="count" data-testid="player-count">
		{fill(t.playerCount, { n: s.roomPlayers.length, max: s.maxPlayers })}
	</p>

	<ul class="seats">
		{#each s.roomPlayers as player (player.playerId)}
			<!-- Owner and ready are different facts about a seat, so they are
			     drawn differently: the owner has no readiness to declare, and
			     painting their row as ready made the highlight read as a claim
			     nobody had made. -->
			<li
				class="seat"
				class:ready={player.ready}
				class:owner={player.isOwner}
				class:me={player.isMe}
			>
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
					     is what an owner who wants out does, and it hands the room on.

					     Two presses rather than a confirm() dialog: the native one
					     blocks the frame loop, and this is the same control asking
					     again rather than a second one appearing. -->
					{#if game.isOwner && !player.isMe}
						<button
							type="button"
							class="kick"
							class:arming={armedKick === player.playerId}
							disabled={player.ready}
							aria-label={armedKick === player.playerId ? t.kickSure : t.kickPlayer}
							data-testid={`kick-${player.playerId}`}
							onclick={() => armOrKick(player.playerId)}
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

	<!-- The grace countdown for a player who dropped. Rendered here too, not
	     only on the board: a room waits in the lobby as often as it plays in
	     it, and "Mất kết nối" on a row says nothing about how long the seat is
	     held. -->
	{#if !compact}
		<PlayerStatus />
	{/if}

	<p class="hint">
		{#if ownerAway}
			{t.ownerAway}
		{:else if game.isOwner && shortHanded}
			{fill(t.ownerNeedsMore, { n: s.minPlayers })}
		{:else if game.isOwner}
			{t.ownerStartsHint}
		{:else if game.isReady}
			{t.waitingForStart}
		{:else}
			{t.guestReadyHint}
		{/if}
	</p>

	<!--
		Where the button is, not in the chat panel below the fold. Every refusal
		the lobby can produce — not_everyone_ready, too_fast, must_unready_first,
		and the reload-the-page protocol mismatch — used to land on a screen the
		player had already scrolled past, which made "Bắt đầu" look broken.
	-->
	{#if s.error}
		<p class="error" role="alert" data-testid="lobby-error">
			{s.error}
			<button
				type="button"
				class="icon-button"
				onclick={() => game.clearError()}
				aria-label={t.dismiss}>×</button
			>
		</p>
	{:else if unsent}
		<p class="error" role="alert" data-testid="lobby-unsent">{t.reconnecting}</p>
	{/if}

	<div class="actions">
		{#if game.isOwner}
			<button
				type="button"
				class="primary"
				disabled={!s.canStart || offline}
				data-testid="start-game"
				onclick={() => report(onstart())}
			>
				{t.startGame}
			</button>
		{:else}
			<button
				type="button"
				class="primary"
				class:on={game.isReady}
				disabled={offline}
				data-testid="ready"
				onclick={() => report(onready(!game.isReady))}
			>
				{game.isReady ? t.unready : t.ready}
			</button>
		{/if}
	</div>

	<!-- Disabled rather than hidden while ready: the rule is worth seeing, and
	     a button that vanishes reads as a bug. Not gated on the connection —
	     giving up the seat is something the player can always do locally. -->
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
		gap: var(--space-3);
	}

	.top {
		display: flex;
		align-items: center;
	}

	.count {
		margin: 0;
		color: var(--text-muted);
		font-size: var(--text-4);
		font-weight: 600;
	}

	.seats {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.seat {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 10px;
		/* A seat is a row to read, not a target to hit, so it keeps the
		   type and gives up the padding: four of them decide whether the
		   buttons under the list are on screen. */
		padding: var(--space-2) 14px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
	}

	/* Tighter still under a finished game, where the list is competing with a
	   result panel for the same screen. */
	.compact .seat {
		padding: var(--space-1) 14px;
	}

	/* Readiness is a tint. Being the owner is a marker down the edge: a fact
	   about the seat, not a state it has entered. */
	.seat.ready {
		border-color: var(--accent);
		background: var(--accent-soft);
	}

	.seat.owner {
		border-inline-start: 3px solid var(--accent);
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
		padding: 1px var(--space-2);
		border-radius: var(--radius-pill);
		background: var(--surface-alt);
		color: var(--text-muted);
		font-size: var(--text-2);
	}

	/* One right-hand group, so a row keeps its shape whether or not it has a
	   readiness to show and whether or not the reader may kick it. */
	.right {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		margin-left: auto;
	}

	.wins {
		color: var(--text-muted);
		font-size: var(--text-2);
		white-space: nowrap;
	}

	.wins strong {
		color: var(--text);
		font-size: var(--text-5);
		font-variant-numeric: tabular-nums;
	}

	.state {
		color: var(--text-muted);
		font-size: var(--text-3);
	}

	.state.offline {
		color: var(--danger);
	}

	/*
	 * 36px of drawn button, because a 44px circle in every seat row would add
	 * a quarter of a screen to a four-seat lobby that is already long. The
	 * touch target is the full 44 all the same, expanded out of the flow by a
	 * pseudo-element so the row keeps its height.
	 */
	.kick {
		position: relative;
		width: 36px;
		height: 36px;
		margin: -4px 0;
		padding: 0;
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-pill);
		background: transparent;
		color: var(--text-muted);
		font-size: var(--text-6);
		line-height: 1;
	}

	.kick::after {
		content: '';
		position: absolute;
		inset: -4px;
	}

	.kick:disabled {
		opacity: 0.35;
	}

	.kick.arming {
		border-color: var(--danger);
		background: var(--danger-soft);
		color: var(--danger);
		font-weight: 700;
	}

	.hint {
		margin: 0;
		color: var(--text-muted);
		font-size: var(--text-5);
		text-align: center;
	}

	.error {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		margin: 0;
		padding: 10px var(--space-3);
		border-radius: var(--radius-sm);
		background: var(--danger-soft);
		color: var(--danger);
		font-size: var(--text-5);
	}

	.actions {
		display: flex;
		gap: var(--space-2);
	}

	.actions button {
		flex: 1;
		/* Still a full 44px: readying and starting are what a player came
		   here to press. */
		min-height: 44px;
		padding: 10px var(--space-3);
		border: 1px solid var(--border-strong);
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
		min-height: 36px;
		padding: var(--space-1) var(--space-4);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--text-muted);
		font-size: var(--text-4);
	}

	.leave:disabled {
		opacity: 0.5;
	}

	.note {
		margin: 0;
		color: var(--text-muted);
		font-size: var(--text-3);
		text-align: center;
	}
</style>
