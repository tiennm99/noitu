<script>
	import { fraction, remainingMs, seconds } from '$lib/countdown.js';
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';
	import { Status, connection, serverNow } from '$lib/ws/connection.svelte.js';

	const RADIUS = 34;
	const CIRCUMFERENCE = 2 * Math.PI * RADIUS;
	const URGENT_SECONDS = 5;
	/** The two marks worth speaking. Announcing every second would be unusable. */
	const SPOKEN_SECONDS = [10, 5];

	let now = $state(serverNow());

	const running = $derived(game.state.phase === 'playing' && game.state.deadlineMs > 0);
	// Whose clock this is. The server rewrites the deadline on every turn
	// handover, so without this the ring counts the opponent's time down and
	// reddens at five seconds — a panic spike, every turn, about nothing the
	// player can act on.
	const mine = $derived(running && game.state.myTurn);
	// A clock drawn over a socket that cannot carry a word is not a clock the
	// player can beat, and pretending otherwise is worse than saying so.
	const stalled = $derived(running && connection.status !== Status.OPEN);

	// requestAnimationFrame rather than an interval: the arc is a continuous
	// value, and a browser that backgrounds the tab stops the loop, which is
	// exactly right for something purely visual. The loop runs only while a turn
	// is being timed — between games it would be 60 invalidations a second of a
	// number that is not moving.
	$effect(() => {
		if (!running) return;
		let frame = requestAnimationFrame(function tick() {
			now = serverNow();
			frame = requestAnimationFrame(tick);
		});
		return () => cancelAnimationFrame(frame);
	});

	const remaining = $derived(running ? remainingMs(game.state.deadlineMs, now) : 0);
	const left = $derived(seconds(remaining));
	const filled = $derived(running ? fraction(remaining, game.state.turnLimitMs) : 1);
	const urgent = $derived(mine && !stalled && left <= URGENT_SECONDS);
	// Derived from the second rather than from `now`: the string changes once a
	// second, and computing it inside the markup ran a replace and an
	// allocation on all sixty frames.
	const label = $derived(fill(t.secondsLeft, { n: left }));

	// role="timer" is implicitly aria-live="off", which is right — sixty
	// announcements a second would be unusable — so a screen-reader player is
	// told nothing at all. This says it twice per turn, on their own turn only.
	const spoken = $derived(mine && SPOKEN_SECONDS.includes(left) ? left : 0);
</script>

<div
	class="ring"
	class:mine
	class:urgent
	class:stalled
	class:idle={!running}
	role="timer"
	aria-label={label}
>
	<svg viewBox="0 0 80 80" aria-hidden="true">
		<circle class="track" cx="40" cy="40" r={RADIUS} />
		<circle
			class="arc"
			cx="40"
			cy="40"
			r={RADIUS}
			stroke-dasharray={CIRCUMFERENCE}
			stroke-dashoffset={CIRCUMFERENCE * (1 - filled)}
		/>
	</svg>
	<span class="value">{running ? left : '–'}</span>
</div>

<p class="sr-only" role="status" aria-live="polite">
	{spoken ? fill(t.yourTimeLeft, { n: spoken }) : ''}
</p>

<style>
	.ring {
		position: relative;
		width: 80px;
		height: 80px;
		/* Somebody else's clock: shown, because the turn is timed and that is
		   worth seeing, but not addressed to this player. */
		color: var(--text-muted);
	}

	.ring.mine {
		color: var(--accent);
	}

	.ring.urgent {
		color: var(--danger);
	}

	/* Between turns there is no clock to show, so the ring reads as waiting
	   rather than as a timer that has run out. */
	.ring.idle {
		color: var(--text-muted);
	}

	.ring.stalled {
		opacity: 0.4;
	}

	svg {
		width: 100%;
		height: 100%;
		/* Start the arc at twelve o'clock and run it clockwise. */
		transform: rotate(-90deg);
	}

	circle {
		fill: none;
		stroke-width: 6;
		stroke-linecap: round;
	}

	/*
	 * Green to red is the pair a red-green colour blindness confuses, so
	 * roughly one man in twelve would get no warning at all. The ring thickens
	 * and the number grows with it: both survive greyscale, and neither moves.
	 */
	.ring.urgent circle {
		stroke-width: 9;
	}

	.ring.urgent .value {
		font-size: 1.75rem;
	}

	.track {
		stroke: var(--border-strong);
	}

	.arc {
		stroke: currentColor;
	}

	.value {
		position: absolute;
		inset: 0;
		display: grid;
		place-items: center;
		color: var(--text);
		font-size: 1.5rem;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
	}

	.ring.idle .value {
		color: var(--text-muted);
	}
</style>
