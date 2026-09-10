<script>
	import { t } from '$lib/i18n/vi.js';
	import { Status, connection } from '$lib/ws/connection.svelte.js';

	const label = $derived(
		{
			[Status.CONNECTING]: t.connecting,
			[Status.OPEN]: t.connected,
			[Status.RECONNECTING]: t.reconnecting,
			[Status.CLOSED]: t.noConnection
		}[connection.status] ?? t.noConnection
	);
</script>

<!--
	aria-live, because losing the connection is the one status change a player
	needs told rather than shown: they may be looking at the input, not here.
-->
<p class="badge" data-status={connection.status} aria-live="polite">
	<span class="dot" aria-hidden="true"></span>
	{label}
</p>

<style>
	.badge {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		margin: 0;
		padding: 4px 10px;
		border-radius: var(--radius-pill);
		background: var(--surface-alt);
		color: var(--text-muted);
		font-size: var(--text-4);
	}

	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: currentColor;
	}

	.badge[data-status='open'] {
		color: var(--accent);
	}

	.badge[data-status='reconnecting'],
	.badge[data-status='connecting'] {
		color: var(--warn);
	}

	.badge[data-status='closed'] {
		color: var(--danger);
	}

	.badge[data-status='reconnecting'] .dot {
		animation: pulse 1.2s ease-in-out infinite;
	}

	@keyframes pulse {
		50% {
			opacity: 0.25;
		}
	}
</style>
