<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type Health } from '$lib/api';
	import HealthBadge from '$lib/components/HealthBadge.svelte';
	import Icon from '$lib/components/Icon.svelte';

	let health: Health | null = null;
	let error = '';
	let loading = true;
	let refreshAt = '';

	async function load() {
		loading = true;
		error = '';
		try {
			health = await api.getHealth();
			refreshAt = new Date().toLocaleTimeString();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	onMount(load);

	function formatUptime(s: number): string {
		if (s < 60) return `${s}s`;
		const m = Math.floor(s / 60);
		if (m < 60) return `${m}m`;
		const h = Math.floor(m / 60);
		const rem = m % 60;
		if (h < 24) return rem > 0 ? `${h}h ${rem}m` : `${h}h`;
		const d = Math.floor(h / 24);
		const remH = h % 24;
		return remH > 0 ? `${d}d ${remH}h` : `${d}d`;
	}
</script>

<svelte:head>
	<title>Overview — Synapto Admin</title>
</svelte:head>

<header class="page-head">
	<div>
		<p class="eyebrow">Overview</p>
		<h1>Service health</h1>
		<p class="lede">Live snapshot of the Synapto assistant service.</p>
	</div>
	<div class="head-actions">
		<button class="btn-secondary" on:click={load} disabled={loading} aria-label="Refresh">
			<Icon name="spinner" size={14} strokeWidth={2.5} />
			<span>{loading ? 'Refreshing…' : 'Refresh'}</span>
		</button>
	</div>
</header>

{#if error}
	<div class="alert danger" role="alert">
		<Icon name="alert" size={18} />
		<div>
			<strong>Cannot reach service.</strong>
			<span>{error}</span>
		</div>
	</div>
{:else if health}
	<HealthBadge
		status={health.status}
		dbOk={health.db_ok}
		lastSuccessfulCycleAt={health.last_successful_cycle_at}
		lastFailureAt={health.last_failure_at}
		lastFailureReason={health.last_failure_reason}
		version={health.version}
		schedulerState={health.scheduler_state}
	/>

	<section class="kpi-grid" aria-label="Operational metrics">
		<article class="kpi-card surface">
			<header>
				<span class="kpi-label">Uptime</span>
				<span class="kpi-mono">{formatUptime(health.uptime_seconds)}</span>
			</header>
			<p class="kpi-meta">Last refresh: {refreshAt || '—'}</p>
		</article>
	</section>

	<section class="surface links" aria-label="Quick links">
		<h2>Quick links</h2>
		<div class="links-grid">
			<a class="link-card" href="/channels">
				<span class="link-icon" aria-hidden="true">
					<Icon name="channel" size={20} />
				</span>
				<span>
					<strong>Manage channels</strong>
					<small>Add or remove the Telegram channels the assistant monitors.</small>
				</span>
				<Icon name="arrow-right" size={16} />
			</a>
			<a class="link-card" href="/categories">
				<span class="link-icon" aria-hidden="true">
					<Icon name="tag" size={20} />
				</span>
				<span>
					<strong>Customize categories</strong>
					<small>Rename, add, or remove the labels used to group digest items.</small>
				</span>
				<Icon name="arrow-right" size={16} />
			</a>
			<a class="link-card" href="/settings">
				<span class="link-icon" aria-hidden="true">
					<Icon name="cog" size={20} />
				</span>
				<span>
					<strong>Settings & probes</strong>
					<small
						>Adjust the digest interval, subscriber chat, and AI/Telegram credentials.</small
					>
				</span>
				<Icon name="arrow-right" size={16} />
			</a>
		</div>
	</section>
{/if}

<style>
	.page-head {
		display: flex;
		justify-content: space-between;
		align-items: flex-end;
		gap: var(--space-4);
		margin-bottom: var(--space-6);
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
		max-width: 50ch;
	}

	.head-actions {
		display: flex;
		gap: 0.5rem;
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
		transition:
			background var(--duration-base) var(--ease-out),
			border-color var(--duration-base) var(--ease-out);
	}
	.btn-secondary:hover:not(:disabled) {
		background: var(--color-surface-2);
		border-color: var(--color-border-strong);
	}
	.btn-secondary:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.kpi-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: var(--space-4);
		margin: var(--space-5) 0;
	}
	.kpi-card {
		padding: var(--space-4);
	}
	.kpi-card header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 0.5rem;
		margin-bottom: 0.4rem;
	}
	.kpi-label {
		font-size: var(--text-xs);
		font-weight: 600;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--color-text-subtle);
	}
	.kpi-mono {
		font-family: var(--font-mono);
		font-size: var(--text-base);
		font-weight: 600;
		color: var(--color-text);
		text-align: right;
	}
	.kpi-meta {
		margin: 0;
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
	}

	.links {
		padding: var(--space-5);
	}
	.links h2 {
		font-size: var(--text-lg);
		margin: 0 0 var(--space-3);
	}
	.links-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
		gap: var(--space-3);
	}
	.link-card {
		display: flex;
		align-items: center;
		gap: 0.7rem;
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		color: var(--color-text);
		text-decoration: none;
		transition:
			border-color var(--duration-base) var(--ease-out),
			background var(--duration-base) var(--ease-out);
	}
	.link-card:hover {
		border-color: var(--color-primary);
		background: var(--color-primary-soft);
		text-decoration: none;
	}
	.link-card:focus-visible {
		outline: none;
		box-shadow: var(--focus-ring);
	}
	.link-icon {
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
	.link-card > span:nth-child(2) {
		display: flex;
		flex-direction: column;
		gap: 2px;
		flex: 1;
		min-width: 0;
	}
	.link-card strong {
		font-size: var(--text-sm);
		font-weight: 600;
		color: var(--color-text);
	}
	.link-card small {
		font-size: var(--text-xs);
		color: var(--color-text-muted);
		line-height: 1.35;
	}
	.link-card > :global(svg:last-child) {
		color: var(--color-text-subtle);
	}

	.alert {
		display: flex;
		gap: 0.75rem;
		padding: var(--space-3) var(--space-4);
		border-radius: var(--radius-md);
		border: 1px solid;
		margin-bottom: var(--space-5);
	}
	.alert.danger {
		background: var(--color-danger-soft);
		border-color: var(--color-danger);
		color: var(--color-danger);
	}
	.alert strong {
		display: block;
		font-weight: 600;
	}
	.alert span {
		font-size: var(--text-sm);
		color: var(--color-text);
	}
</style>
