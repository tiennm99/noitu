<script>
	import RoomCodePanel from '$lib/components/RoomCodePanel.svelte';
	import { t } from '$lib/i18n/vi.js';
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
	 *   onkick: () => void,
	 *   onleave: () => void
	 * }}
	 */
	let { compact = false, onready, onstart, onkick, onleave } = $props();

	const s = $derived(game.state);
</script>

<section class="lobby" aria-label={t.lobbyTitle}>
	{#if !compact}
		<RoomCodePanel code={s.roomCode} />
	{/if}

	<ul class="seats">
		<li class="seat" class:ready={s.isOwner || s.isReady}>
			<span class="name">{s.nickname || t.you}</span>
			<span class="role">{s.isOwner ? t.owner : t.guest}</span>
			<!-- The owner has no readiness to show: starting is the statement. -->
			{#if !s.isOwner}
				<span class="state" data-testid="my-ready">{s.isReady ? t.isReady : t.notReady}</span>
			{/if}
		</li>

		<li class="seat" class:empty={!s.opponentPresent} class:ready={s.opponentReady}>
			{#if s.opponentPresent}
				<span class="name">{s.opponentName || t.opponent}</span>
				<span class="role">{s.isOwner ? t.guest : t.owner}</span>
				{#if !s.opponentConnected}
					<span class="state offline">{t.offline}</span>
				{:else if s.isOwner}
					<span class="state" data-testid="opponent-ready">
						{s.opponentReady ? t.isReady : t.notReady}
					</span>
				{/if}
			{:else}
				<span class="name muted">{t.emptySeat}</span>
			{/if}
		</li>
	</ul>

	<p class="hint">
		{#if s.isOwner}
			{s.opponentPresent ? t.ownerStartsHint : t.waitingForOpponent}
		{:else if s.isReady}
			{t.waitingForStart}
		{:else}
			{t.guestReadyHint}
		{/if}
	</p>

	<div class="actions">
		{#if s.isOwner}
			<button
				type="button"
				class="primary"
				disabled={!s.canStart}
				data-testid="start-game"
				onclick={onstart}
			>
				{t.startGame}
			</button>
			{#if s.opponentPresent}
				<button type="button" disabled={s.opponentReady} onclick={onkick}>
					{t.kickPlayer}
				</button>
			{/if}
		{:else}
			<button
				type="button"
				class="primary"
				class:on={s.isReady}
				data-testid="ready"
				onclick={() => onready(!s.isReady)}
			>
				{s.isReady ? t.unready : t.ready}
			</button>
		{/if}
	</div>

	<!-- Disabled rather than hidden while ready: the rule is worth seeing, and
	     a button that vanishes reads as a bug. -->
	<button type="button" class="leave" disabled={s.isReady} onclick={onleave}>
		{t.leaveRoom}
	</button>
	{#if s.isReady}
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

	.state {
		margin-left: auto;
		color: var(--text-muted);
		font-size: 0.8rem;
	}

	.state.offline {
		color: var(--danger);
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
