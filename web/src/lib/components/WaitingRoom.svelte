<script>
	import RoomCodePanel from '$lib/components/RoomCodePanel.svelte';
	import { t } from '$lib/i18n/vi.js';

	/** @type {{ code: string, onleave: () => void }} */
	let { code, onleave } = $props();
</script>

<section class="waiting">
	<RoomCodePanel {code} />

	<p class="status" role="status" data-testid="waiting-status">
		<span class="spinner" aria-hidden="true"></span>
		{t.waitingForOpponent}
	</p>

	<button type="button" class="leave" onclick={onleave}>{t.leaveRoom}</button>
</section>

<style>
	.waiting {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 16px;
	}

	.status {
		display: flex;
		align-items: center;
		gap: 10px;
		margin: 0;
		color: var(--text-muted);
	}

	.spinner {
		width: 12px;
		height: 12px;
		border: 2px solid var(--border);
		border-top-color: var(--accent);
		border-radius: 50%;
		animation: spin 0.9s linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	.leave {
		padding: 10px 18px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--text-muted);
		font-size: 0.9rem;
	}
</style>
