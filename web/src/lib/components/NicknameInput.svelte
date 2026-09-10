<script>
	import { t } from '$lib/i18n/vi.js';
	import { MAX_NICKNAME_LENGTH, settings } from '$lib/stores/settings.svelte.js';

	const id = 'nickname-input';
</script>

<div class="field">
	<label for={id}>{t.nicknameLabel}</label>
	<!--
		The value is never written back while the player types. A Telex or VNI
		IME composes a diacritic across several keystrokes, and reassigning the
		field mid-composition drops the accent being formed. The cap is applied
		in the store, which is also where the same rule is unit-tested.
	-->
	<input
		{id}
		type="text"
		autocomplete="nickname"
		maxlength={MAX_NICKNAME_LENGTH}
		placeholder={t.nicknamePlaceholder}
		value={settings.state.nickname}
		oninput={(event) => settings.setNickname(event.currentTarget.value)}
	/>
	<p class="hint">{t.nicknameHint}</p>
</div>

<style>
	.field {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}

	label {
		font-weight: 600;
		font-size: var(--text-5);
	}

	input {
		padding: var(--space-3) 14px;
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: var(--surface);
		font-size: var(--text-6);
	}

	.hint {
		margin: 0;
		color: var(--text-muted);
		font-size: var(--text-3);
	}
</style>
