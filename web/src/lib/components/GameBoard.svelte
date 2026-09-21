<script>
	import ArmedButton from '$lib/components/ArmedButton.svelte';
	import ChainHistory from '$lib/components/ChainHistory.svelte';
	import ConnectionBadge from '$lib/components/ConnectionBadge.svelte';
	import CountdownRing from '$lib/components/CountdownRing.svelte';
	import ScoreBoard from '$lib/components/ScoreBoard.svelte';
	import WordInput from '$lib/components/WordInput.svelte';
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';
	import { Status, connection, reconnectNow } from '$lib/ws/connection.svelte.js';

	/**
	 * The board itself, shared by both modes. It renders the store and nothing
	 * else; what a finished game offers differs between bot and online play, so
	 * that arrives as a snippet rather than as a branch in here.
	 *
	 * `chatUnread` and `onchatopen` are online-only: a bot game has no chat, so
	 * the caller simply never passes them, and the pill they draw does not
	 * appear. Passing them is what puts a way to reach the conversation above
	 * the chain rather than under it, which on a phone mid-game used to make
	 * the unread count effectively unreachable.
	 * @type {{
	 *   modeLabel?: string,
	 *   onsubmit: (word: string) => boolean,
	 *   onresign: () => void,
	 *   onclaimdeadend: () => void,
	 *   onreportword: (word: string) => void,
	 *   gameOver: import('svelte').Snippet,
	 *   banner?: import('svelte').Snippet,
	 *   chatUnread?: number,
	 *   onchatopen?: () => void
	 * }}
	 */
	let {
		modeLabel = '',
		onsubmit,
		onresign,
		onclaimdeadend,
		onreportword,
		gameOver,
		banner,
		chatUnread = 0,
		onchatopen
	} = $props();

	// Whose turn it is, said by name. With four people at the table "the
	// opponent is thinking" stops naming anybody.
	const turnLabel = $derived.by(() => {
		if (game.state.myTurn) return t.yourTurn;
		const name = game.nameOf(game.state.turnPlayerId);
		return name ? fill(t.playerTurn, { name }) : t.opponentTurn;
	});

	const offline = $derived(connection.status !== Status.OPEN);

	// Giving up is a move: it is what a player plays instead of a word, so it
	// is offered on their turn and no other. Out of turn the way out of a game
	// is to leave the room, which the lobby's own button does.
	const canResign = $derived(game.state.myTurn && !offline);

	// A claim is the same offer resign is: made only on the player's own turn,
	// on the same reasoning canResign already states.
	const canClaimDeadEnd = $derived(game.state.myTurn && !offline);
</script>

<section class="board" data-phase={game.state.phase}>
	<div class="top">
		<!-- Its text label is what crowded this row: "Đã kết nối" said nothing
		     a player needed while it stayed true, and the moment it stops
		     being true is exactly when the label earns its width back. -->
		<ConnectionBadge compact />
		<div class="meta">
			<!-- A new tab: this screen resigns or leaves the room when it unmounts,
			     so an in-page navigation to the rules would forfeit the game. A
			     44px glyph rather than the underlined text link used elsewhere:
			     at 360px "Luật chơi" was a third of the row on its own. -->
			<a
				class="icon-button"
				href="/rules"
				target="_blank"
				rel="noopener"
				aria-label={t.rulesLink}>?</a
			>
			{#if onchatopen}
				<!-- Above the chain rather than below it, which is where this used
				     to live: the chain grows a row per turn, and a badge under it
				     was two screens down by the time a game was worth talking
				     about. Scrolling the panel into view rather than opening it in
				     place, since a folded panel scrolled here still shows its own
				     badge and its own way to unfold. -->
				<!-- Its own accessible name: the panel below has a toggle that
				     reads "Trò chuyện" too, and two controls announced alike are
				     one control a screen reader cannot tell apart. -->
				<button
					type="button"
					class="chat-pill"
					onclick={onchatopen}
					aria-label={chatUnread > 0
						? `${t.chatOpen}, ${fill(t.chatUnread, { n: chatUnread })}`
						: t.chatOpen}
					data-testid="chat-pill"
				>
					<span aria-hidden="true">💬</span>
					<span class="sr-only">{t.chatTitle}</span>
					{#if chatUnread > 0}
						<span class="pill-badge">{chatUnread}</span>
					{/if}
				</button>
			{/if}
		</div>
	</div>

	<!-- Not once the game is over: the result panel below carries the same
	     names and scores as a ranked table, and two of them is one screenful
	     of duplication between the result and the lobby's own buttons. -->
	{#if game.state.phase !== 'over'}
		<ScoreBoard />
	{/if}

	{#if banner}{@render banner()}{/if}

	<!--
		Not a live region: the connection badge above already announces the same
		change, and saying it twice is worse than saying it once. This is the
		visible half — and the only place the player can do anything about it,
		since the backoff otherwise runs up to eight seconds while their turn
		does not wait.
	-->
	{#if offline && game.state.phase === 'playing'}
		<p class="offline">
			{t.reconnecting}
			<button type="button" onclick={() => reconnectNow()}>{t.retry}</button>
		</p>
	{/if}

	{#if game.state.error}
		<p class="error" role="alert">
			{game.state.error}
			<button
				type="button"
				class="icon-button"
				onclick={() => game.clearError()}
				aria-label={t.dismiss}>×</button
			>
		</p>
	{/if}

	{#if game.state.phase === 'over'}
		{@render gameOver()}
	{:else}
		<div class="turn">
			<CountdownRing />
			<div class="prompt">
				<!--
					The turn changing is the one state change the whole game hangs
					on, and it reached nobody who was not looking at this line: a
					screen-reader player, or anybody reading the chat beside the
					board, learned it from the clock forfeiting them.
				-->
				<p
					class="who"
					class:mine={game.state.myTurn}
					role="status"
					aria-live="polite"
					aria-atomic="true"
					data-testid="turn-indicator"
				>
					{turnLabel}
				</p>
				<p class="syllable">
					<span class="label">{t.currentSyllable}</span>
					<strong data-testid="current-syllable">{game.state.currentSyllable || '…'}</strong>
				</p>
				<!-- The room code (or, in a bot game, the difficulty): a room fact
				     rather than a turn fact, so it sits under the prompt it used to
				     crowd in the header rather than beside the connection badge. -->
				{#if modeLabel}<p class="mode">{modeLabel}</p>{/if}
			</div>
		</div>

		<!-- A player who has been knocked out watches the rest of it: the chain,
		     the clock and the chat all keep working, and only the one thing
		     they can no longer do goes away. The board said so twice before —
		     this box and a banner above the scoreboard — so the elimination
		     suggestions move in here rather than waiting for the game-over
		     screen, which a four-seat spectator can be minutes away from. -->
		{#if game.iAmOut}
			<div class="spectating" role="status" data-testid="eliminated">
				<p>{t.youAreOut}</p>
				{#if game.state.elimination?.suggestions.length}
					<p class="could">
						{t.suggestionsTitle}: {game.state.elimination.suggestions.join(' · ')}
					</p>
				{:else if game.state.elimination}
					<p class="could">
						{fill(t.noSuggestions, { syllable: game.state.elimination.syllable })}
					</p>
				{/if}
			</div>
		{:else}
			<WordInput {onsubmit} {onreportword} />
		{/if}
	{/if}

	<!-- One row, both controls always mounted: the "Bí từ" button used to
	     mount and unmount with every handover, shifting the input under a
	     player's thumb each turn — the same churn `.resign` was already
	     built to avoid. Disabled off-turn instead, which keeps the row's
	     height constant. -->
	{#if game.state.phase === 'playing' && !game.iAmOut}
		<div class="secondary">
			<ArmedButton
				class="claim-dead-end"
				label={t.claimDeadEnd}
				confirmLabel={t.claimDeadEndSure}
				disabled={!canClaimDeadEnd}
				onconfirm={onclaimdeadend}
			/>
			<ArmedButton
				class="resign"
				label={t.resign}
				confirmLabel={t.resignSure}
				disabled={!canResign}
				onconfirm={onresign}
			/>
		</div>
		{#if game.state.claimError}
			<p class="claim-error" role="alert">
				{game.state.claimError}
				<button
					type="button"
					class="icon-button"
					onclick={() => game.clearClaimError()}
					aria-label={t.dismiss}>×</button
				>
			</p>
		{/if}
	{/if}

	<ChainHistory />
</section>

<style>
	.board {
		display: flex;
		flex-direction: column;
		flex: 1;
		gap: var(--space-3);
		min-height: 0;
		padding-bottom: var(--space-2);
	}

	.top {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}

	.meta {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: flex-end;
		gap: var(--space-3);
	}

	.mode {
		margin: var(--space-1) 0 0;
		color: var(--text-muted);
		font-size: var(--text-1);
	}

	.chat-pill {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		min-height: 32px;
		min-width: 44px;
		justify-content: center;
		padding: 4px var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-pill);
		background: var(--surface-alt);
		font-size: var(--text-2);
		font-weight: 600;
	}

	.pill-badge {
		padding: 1px var(--space-2);
		border-radius: var(--radius-pill);
		background: var(--accent);
		color: var(--accent-text);
		font-size: var(--text-1);
		font-weight: 700;
	}

	.turn {
		display: flex;
		align-items: center;
		gap: var(--space-4);
	}

	.prompt {
		min-width: 0;
	}

	.who {
		margin: 0 0 var(--space-1);
		color: var(--text-muted);
		font-size: var(--text-2);
	}

	/* The player's own turn, said loudly enough to catch the eye that is in the
	   chat column beside the board. */
	.who.mine {
		color: var(--text);
		font-size: var(--text-3);
		font-weight: 700;
	}

	.syllable {
		display: flex;
		flex-direction: column;
		margin: 0;
	}

	.syllable .label {
		margin-bottom: 2px;
		color: var(--text-muted);
		font-size: var(--text-1);
	}

	/* The one glyph read every single turn, so it gets the headroom: a stacked
	   Vietnamese tone mark on ệ or ộ rides into the label above it at 1.2. */
	.syllable strong {
		font-size: var(--text-7);
		line-height: 1.35;
	}

	.error,
	.offline {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		margin: 0;
		padding: var(--space-3) var(--space-3);
		border-radius: var(--radius-sm);
		font-size: var(--text-2);
	}

	.error {
		background: var(--danger-soft);
		color: var(--danger);
	}

	.offline {
		background: var(--surface-alt);
		color: var(--warn);
	}

	.offline button {
		flex: none;
		min-height: 44px;
		padding: var(--space-2) var(--space-4);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		margin: -8px 0;
		background: var(--surface);
		color: var(--text);
		font-size: var(--text-2);
		font-weight: 600;
	}

	.spectating {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		margin: 0;
		padding: var(--space-3);
		border: 1px dashed var(--border);
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		text-align: center;
	}

	.spectating p {
		margin: 0;
	}

	/* The one thing worth reading in this box once the news itself has sunk
	   in: what would have gotten this player out of the position that beat
	   them. */
	.spectating .could {
		color: var(--text);
	}

	/* Both controls on one row now, so alignment comes from the row rather
	   than from each button placing itself at an end of the column. */
	.secondary {
		display: flex;
		justify-content: space-between;
		gap: var(--space-2);
	}

	/* :global(): these are ArmedButton's own <button>, not one this
	   component's template renders directly, so Svelte's scoped-style
	   attribute never lands on it. */

	/* Danger coloured because it ends the game, subordinate because it is not
	   the way to play it. */
	:global(.resign) {
		/* Below the 44px the rest of the controls keep, deliberately: this is
		   the one button here nobody is trying to hit, it takes two presses to
		   do anything, and at full size it read as an offer rather than as the
		   way out. Still its own outlined block in danger colour, so it is
		   plainly findable rather than hidden. */
		min-height: 32px;
		padding: var(--space-1) var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--danger);
		font-size: var(--text-2);
		transition: background-color 150ms ease-out;
	}

	:global(.resign:hover:enabled) {
		background: var(--danger-soft);
	}

	/* Off turn: still there, so the way out of the game does not appear and
	   disappear under the player's thumb every handover, but plainly not the
	   thing to press yet. */
	:global(.resign:disabled) {
		border-color: var(--border);
		color: var(--text-muted);
	}

	/* Armed, and saying so: the second press is the one that ends the game. */
	:global(.resign.arming) {
		border-color: var(--danger);
		background: var(--danger-soft);
		font-weight: 600;
	}

	/* Secondary weight, same as resign: a dead-end claim is a shortcut past a
	   turn that cannot be answered, not the way to play one. */
	:global(.claim-dead-end) {
		min-height: 32px;
		padding: var(--space-1) var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--text);
		font-size: var(--text-2);
		transition: background-color 150ms ease-out;
	}

	:global(.claim-dead-end:hover:enabled) {
		background: var(--surface-alt);
	}

	:global(.claim-dead-end:disabled) {
		border-color: var(--border);
		color: var(--text-muted);
	}

	:global(.claim-dead-end.arming) {
		border-color: var(--accent);
		background: var(--accent-soft);
		font-weight: 600;
	}

	.claim-error {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		margin: 0;
		padding: var(--space-3) var(--space-3);
		border-radius: var(--radius-sm);
		background: var(--danger-soft);
		color: var(--danger);
		font-size: var(--text-2);
	}
</style>
