<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type Settings } from '$lib/api';
	import Icon from '$lib/components/Icon.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';

	let settings: Settings | null = null;
	let loading = false;
	let saving = false;
	let error = '';
	let success = '';
	let probeResult: { telegram?: string; ai?: string; probeError?: string } = {};

	// Form state
	let intervalMinutes = 10;
	let intervalSeconds = 0;
	let chatId = 0;
	let uncategorizedLabel = 'Uncategorized';

	$: intervalTotal = intervalMinutes * 60 + intervalSeconds;
	$: intervalValid = intervalTotal >= 60 && intervalTotal <= 86400;
	$: chatIdValid = chatId >= 0;
	$: labelValid = uncategorizedLabel.trim().length > 0 && uncategorizedLabel.length <= 40;
	$: formValid = intervalValid && chatIdValid && labelValid;
	$: formDirty =
		settings !== null &&
		(intervalTotal !== settings.digest_interval_seconds ||
			chatId !== settings.telegram_subscriber_chat ||
			uncategorizedLabel !== settings.uncategorized_label);

	onMount(load);

	async function load() {
		loading = true;
		error = '';
		try {
			const res = await api.getSettings();
			settings = res.settings;
			applyToForm(res.settings);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	function applyToForm(s: Settings) {
		intervalMinutes = Math.floor(s.digest_interval_seconds / 60);
		intervalSeconds = s.digest_interval_seconds % 60;
		chatId = s.telegram_subscriber_chat;
		uncategorizedLabel = s.uncategorized_label;
	}

	function resetForm() {
		if (settings) applyToForm(settings);
	}

	async function save() {
		if (!formValid || !formDirty) return;
		saving = true;
		error = '';
		success = '';
		try {
			const res = await api.patchSettings({
				digest_interval_seconds: intervalTotal,
				telegram_subscriber_chat: chatId,
				uncategorized_label: uncategorizedLabel.trim()
			});
			settings = res.settings;
			applyToForm(res.settings);
			success = 'Settings saved. The new interval takes effect from the next cycle.';
		} catch (e) {
			error = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function probeTelegram() {
		error = '';
		probeResult = { ...probeResult, telegram: undefined, probeError: undefined };
		try {
			const res = await api.testTelegram();
			probeResult = {
				...probeResult,
				telegram: res.ok ? `@${res.bot.username} (id ${res.bot.id})` : 'failed'
			};
		} catch (e) {
			probeResult = { ...probeResult, probeError: (e as Error).message };
		}
	}

	async function probeAI() {
		error = '';
		probeResult = { ...probeResult, ai: undefined, probeError: undefined };
		try {
			const res = await api.testAI();
			probeResult = {
				...probeResult,
				ai: res.ok ? `${res.model} · ${res.latency_ms}ms` : 'failed'
			};
		} catch (e) {
			probeResult = { ...probeResult, probeError: (e as Error).message };
		}
	}

	function formatDate(s: string | undefined | null): string {
		if (!s) return '—';
		try {
			return new Date(s).toLocaleString();
		} catch {
			return s;
		}
	}
</script>

<svelte:head>
	<title>Settings — Synapto Admin</title>
</svelte:head>

<header class="page-head">
	<div>
		<p class="eyebrow">Operator</p>
		<h1>Settings</h1>
		<p class="lede">
			Configure the digest cadence, the subscriber chat, and how uncategorized items are
			labeled. Changes are persisted immediately and the scheduler picks up the new interval
			within seconds.
		</p>
	</div>
	{#if settings}
		<p class="updated">Last updated: {formatDate(settings.updated_at)}</p>
	{/if}
</header>

{#if error}
	<div class="alert danger" role="alert">
		<Icon name="alert" size={18} />
		<div>
			<strong>Couldn't save settings.</strong>
			<span>{error}</span>
		</div>
	</div>
{/if}
{#if success}
	<div class="alert success" role="status">
		<Icon name="check-circle" size={18} />
		<div>
			<strong>{success}</strong>
		</div>
	</div>
{/if}

{#if loading && !settings}
	<div class="loading surface">
		<Icon name="spinner" size={18} />
		<span>Loading settings…</span>
	</div>
{:else if settings}
	<form class="settings-form" on:submit|preventDefault={save} aria-label="Operator settings">
		<section class="surface group" aria-labelledby="group-cadence">
			<header class="group-head">
				<span class="group-icon" aria-hidden="true">
					<Icon name="cog" size={18} />
				</span>
				<div>
					<h2 id="group-cadence">Cadence</h2>
					<p>How often the assistant fetches new posts and ships a digest.</p>
				</div>
			</header>

			<div class="field">
				<label for="interval-min">Interval</label>
				<div class="interval-row">
					<div class="interval-part">
						<input
							id="interval-min"
							type="number"
							min="1"
							max="1440"
							bind:value={intervalMinutes}
							aria-describedby="interval-hint"
						/>
						<span class="suffix">min</span>
					</div>
					<span class="interval-sep">:</span>
					<div class="interval-part">
						<input
							type="number"
							min="0"
							max="59"
							bind:value={intervalSeconds}
							aria-label="seconds"
						/>
						<span class="suffix">sec</span>
					</div>
				</div>
				<p class="hint" id="interval-hint">
					Total <strong>{intervalTotal}s</strong> · allowed range: 60s to 24h.
					{#if !intervalValid}
						<span class="hint-error">Out of range.</span>
					{/if}
				</p>
			</div>
		</section>

		<section class="surface group" aria-labelledby="group-delivery">
			<header class="group-head">
				<span class="group-icon" aria-hidden="true">
					<Icon name="channel" size={18} />
				</span>
				<div>
					<h2 id="group-delivery">Delivery</h2>
					<p>Where the digest message is sent.</p>
				</div>
			</header>

			<div class="field">
				<label for="chat-id">Subscriber chat ID</label>
				<input
					id="chat-id"
					type="number"
					min="0"
					bind:value={chatId}
					placeholder="e.g. 123456789"
				/>
				<p class="hint">
					The numeric Telegram chat ID of the subscriber. Use 0 to disable sending.
					{#if !chatIdValid}<span class="hint-error">Must be non-negative.</span>{/if}
				</p>
			</div>

			<div class="field">
				<label for="uncategorized-label">Uncategorized label</label>
				<input
					id="uncategorized-label"
					type="text"
					maxlength="40"
					bind:value={uncategorizedLabel}
					placeholder="Uncategorized"
				/>
				<p class="hint">
					Heading used for digest items the AI can't classify into a known category. Max
					40 characters.
				</p>
			</div>
		</section>

		<section class="surface group" aria-labelledby="group-probes">
			<header class="group-head">
				<span class="group-icon" aria-hidden="true">
					<Icon name="info" size={18} />
				</span>
				<div>
					<h2 id="group-probes">Connectivity probes</h2>
					<p>
						Validate the configured Telegram bot and AI provider. Probes do not change
						settings.
					</p>
				</div>
			</header>

			<dl class="probe-grid">
				<div>
					<dt>Telegram</dt>
					<dd>
						{#if settings.telegram_bot_reachable === true}
							<StatusBadge kind="ok" label="reachable" />
						{:else if settings.telegram_bot_reachable === false}
							<StatusBadge kind="danger" label="not reachable" />
						{:else}
							<StatusBadge kind="neutral" label="unknown" />
						{/if}
						<small class="ref"
							>{settings.telegram_bot_token_ref || 'not configured'}</small
						>
					</dd>
				</div>
				<div>
					<dt>AI</dt>
					<dd>
						{#if settings.ai_reachable === true}
							<StatusBadge kind="ok" label="reachable" />
						{:else if settings.ai_reachable === false}
							<StatusBadge kind="danger" label="not reachable" />
						{:else}
							<StatusBadge kind="neutral" label="unknown" />
						{/if}
						<small class="ref">{settings.ai_model} · {settings.ai_provider}</small>
					</dd>
				</div>
			</dl>

			<div class="probe-actions">
				<button type="button" class="btn-secondary" on:click={probeTelegram}>
					<Icon name="channel" size={14} />
					Test Telegram
				</button>
				<button type="button" class="btn-secondary" on:click={probeAI}>
					<Icon name="cog" size={14} />
					Test AI
				</button>
			</div>

			{#if probeResult.telegram || probeResult.ai || probeResult.probeError}
				<div class="probe-results" role="status">
					{#if probeResult.telegram}
						<p><strong>Telegram:</strong> {probeResult.telegram}</p>
					{/if}
					{#if probeResult.ai}
						<p><strong>AI:</strong> {probeResult.ai}</p>
					{/if}
					{#if probeResult.probeError}
						<p class="probe-error">
							<strong>Probe failed:</strong>
							{probeResult.probeError}
						</p>
					{/if}
				</div>
			{/if}
		</section>

		<footer class="form-footer">
			<button
				type="button"
				class="btn-ghost"
				on:click={resetForm}
				disabled={!formDirty || saving}
			>
				Reset
			</button>
			<button type="submit" class="btn-primary" disabled={!formDirty || !formValid || saving}>
				<Icon name="check" size={14} strokeWidth={2.5} />
				{saving ? 'Saving…' : 'Save changes'}
			</button>
		</footer>
	</form>
{/if}

<style>
	.page-head {
		display: flex;
		justify-content: space-between;
		align-items: flex-end;
		gap: var(--space-4);
		margin-bottom: var(--space-5);
		flex-wrap: wrap;
	}
	.eyebrow {
		font-size: var(--text-xs);
		font-weight: 600;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--color-primary);
		margin: 0 0 0.25rem;
	}
	.lede {
		color: var(--color-text-muted);
		margin: 0;
		max-width: 60ch;
		font-size: var(--text-sm);
	}
	.updated {
		margin: 0;
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
		font-family: var(--font-mono);
	}

	.alert {
		display: flex;
		gap: 0.75rem;
		padding: var(--space-3) var(--space-4);
		border-radius: var(--radius-md);
		border: 1px solid;
		margin-bottom: var(--space-3);
	}
	.alert.danger {
		background: var(--color-danger-soft);
		border-color: var(--color-danger);
		color: var(--color-danger);
	}
	.alert.success {
		background: var(--color-success-soft);
		border-color: var(--color-success);
		color: var(--color-success);
	}
	.alert strong {
		display: block;
		font-weight: 600;
	}
	.alert span {
		font-size: var(--text-sm);
		color: var(--color-text);
	}

	.loading {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 0.5rem;
		padding: var(--space-8);
		color: var(--color-text-muted);
		font-size: var(--text-sm);
	}

	.settings-form {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	.group {
		padding: var(--space-4) var(--space-5);
	}
	.group-head {
		display: flex;
		align-items: flex-start;
		gap: 0.75rem;
		padding-bottom: var(--space-3);
		margin-bottom: var(--space-4);
		border-bottom: 1px solid var(--color-border);
	}
	.group-icon {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 36px;
		height: 36px;
		border-radius: var(--radius-md);
		background: var(--color-primary-soft);
		color: var(--color-primary);
		flex-shrink: 0;
	}
	.group-head h2 {
		font-size: var(--text-lg);
		margin: 0 0 0.2rem;
	}
	.group-head p {
		margin: 0;
		font-size: var(--text-sm);
		color: var(--color-text-muted);
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: 0.4rem;
		margin-bottom: var(--space-3);
	}
	.field:last-child {
		margin-bottom: 0;
	}
	.field label {
		font-size: var(--text-sm);
		font-weight: 500;
		color: var(--color-text);
	}

	.interval-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}
	.interval-part {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		flex: 0 0 auto;
	}
	.interval-part input {
		width: 6rem;
		text-align: right;
		font-family: var(--font-mono);
	}
	.suffix {
		font-size: var(--text-sm);
		color: var(--color-text-subtle);
	}
	.interval-sep {
		font-family: var(--font-mono);
		color: var(--color-text-subtle);
	}

	.hint {
		margin: 0.1rem 0 0;
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
	}
	.hint strong {
		font-family: var(--font-mono);
		color: var(--color-text);
	}
	.hint-error {
		color: var(--color-danger);
		margin-left: 0.4rem;
		font-weight: 600;
	}

	.probe-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: var(--space-3);
		margin: 0 0 var(--space-3);
	}
	.probe-grid dt {
		font-size: var(--text-xs);
		font-weight: 600;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--color-text-subtle);
		margin-bottom: 0.3rem;
	}
	.probe-grid dd {
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
	}
	.probe-grid .ref {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--color-text-muted);
	}

	.probe-actions {
		display: flex;
		gap: 0.5rem;
		margin-bottom: var(--space-3);
	}
	.probe-results {
		padding: var(--space-2) var(--space-3);
		background: var(--color-surface-2);
		border-radius: var(--radius-md);
		font-size: var(--text-sm);
	}
	.probe-results p {
		margin: 0;
	}
	.probe-error {
		color: var(--color-danger);
	}

	.form-footer {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
		padding: var(--space-3) 0;
	}
	.btn-primary {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.55rem 1rem;
		background: var(--color-primary);
		color: #fff;
		border: none;
		border-radius: var(--radius-md);
		font-size: var(--text-sm);
		font-weight: 600;
		cursor: pointer;
		transition: background var(--duration-base) var(--ease-out);
	}
	.btn-primary:hover:not(:disabled) {
		background: var(--color-primary-hover);
	}
	.btn-primary:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
	.btn-secondary {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.45rem 0.85rem;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		color: var(--color-text);
		border-radius: var(--radius-md);
		font-size: var(--text-sm);
		font-weight: 500;
		cursor: pointer;
		transition:
			background var(--duration-base) var(--ease-out),
			border-color var(--duration-base) var(--ease-out);
	}
	.btn-secondary:hover:not(:disabled) {
		background: var(--color-surface-2);
		border-color: var(--color-border-strong);
	}
	.btn-ghost {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.55rem 0.85rem;
		background: transparent;
		border: 1px solid var(--color-border);
		color: var(--color-text-muted);
		border-radius: var(--radius-md);
		font-size: var(--text-sm);
		font-weight: 500;
		cursor: pointer;
		transition: background var(--duration-base) var(--ease-out);
	}
	.btn-ghost:hover:not(:disabled) {
		background: var(--color-surface-2);
		color: var(--color-text);
	}
	.btn-ghost:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
</style>
