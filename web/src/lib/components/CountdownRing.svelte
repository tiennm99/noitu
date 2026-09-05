<script>
	import { fraction, remainingMs, seconds } from '$lib/countdown.js';
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';
	import { serverNow } from '$lib/ws/connection.svelte.js';

	const RADIUS = 34;
	const CIRCUMFERENCE = 2 * Math.PI * RADIUS;
	const URGENT_SECONDS = 5;

	let now = $state(serverNow());

	const running = $derived(game.state.phase === 'playing' && game.state.deadlineMs > 0);

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
	const urgent = $derived(running && left <= URGENT_SECONDS);
</script>

<div class="ring" class:urgent class:idle={!running} role="timer" aria-label={fill(t.secondsLeft, { n: left })}>
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

<style>
	.ring {
		position: relative;
		width: 80px;
		height: 80px;
		color: var(--accent);
	}

	.ring.urgent {
		color: var(--danger);
	}

	/* Between turns there is no clock to show, so the ring reads as waiting
	   rather than as a timer that has run out. */
	.ring.idle {
		color: var(--border);
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

	.track {
		stroke: var(--border);
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
