<script lang="ts">
	import Icon from './Icon.svelte';
	import StatusBadge, { type StatusKind } from './StatusBadge.svelte';

	export let status: string = 'unknown';
	export let dbOk: boolean = true;
	export let lastSuccessfulCycleAt: string | null = null;
	export let lastFailureAt: string | null = null;
	export let lastFailureReason: string | null = null;
	export let version: string = '';
	export let schedulerState: string = 'idle';

	$: kind = computeKind(status, dbOk);
	$: label = computeLabel(status, dbOk);

	function computeKind(s: string, db: boolean): StatusKind {
		if (!db) return 'unreachable';
		if (s === 'ok' || s === 'degraded') return 'ok';
		return 'danger';
	}
	function computeLabel(s: string, db: boolean): string {
		if (!db) return 'db error';
		return s;
	}

	function formatDate(iso: string | null): string {
		if (!iso) return 'never';
		try {
			return new Date(iso).toLocaleString();
		} catch {
			return iso;
		}
	}

	function schedulerKind(s: string): StatusKind {
		if (s === 'running') return 'running';
		if (s === 'failed') return 'failed';
		return 'idle';
	}
</script>

<article class="health-badge surface" aria-label="Service health">
	<header class="head">
		<div class="head-left">
			<span class="head-icon" aria-hidden="true">
				<Icon name="info" size={18} />
			</span>
			<div>
				<h2>Service health</h2>
				{#if version}
					<p class="version">v{version}</p>
				{/if}
			</div>
		</div>
		<StatusBadge {kind} {label} size="md" />
	</header>

	<dl class="metrics">
		<div class="metric">
			<dt>Scheduler</dt>
			<dd>
				<StatusBadge kind={schedulerKind(schedulerState)} label={schedulerState} />
			</dd>
		</div>
		<div class="metric">
			<dt>Last successful cycle</dt>
			<dd class="time">{formatDate(lastSuccessfulCycleAt)}</dd>
		</div>
		<div class="metric">
			<dt>Last failure</dt>
			<dd class="time">
				{formatDate(lastFailureAt)}
				{#if lastFailureReason}
					<span class="reason" title={lastFailureReason}>
						<Icon name="warning" size={12} />
						{lastFailureReason}
					</span>
				{/if}
			</dd>
		</div>
	</dl>
</article>

<style>
	.health-badge {
		padding: var(--space-4) var(--space-5);
	}
	.head {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: var(--space-3);
		margin-bottom: var(--space-4);
	}
	.head-left {
		display: flex;
		align-items: center;
		gap: 0.75rem;
	}
	.head-icon {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 36px;
		height: 36px;
		border-radius: var(--radius-md);
		background: var(--color-primary-soft);
		color: var(--color-primary);
	}
	h2 {
		font-size: var(--text-lg);
		font-weight: 600;
		margin: 0;
		line-height: 1.2;
	}
	.version {
		margin: 0.1rem 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
	}
	.metrics {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
		gap: var(--space-3) var(--space-4);
		margin: 0;
	}
	.metric {
		min-width: 0;
	}
	.metric dt {
		font-size: var(--text-xs);
		font-weight: 600;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--color-text-subtle);
		margin-bottom: 0.3rem;
	}
	.metric dd {
		margin: 0;
		font-size: var(--text-sm);
		color: var(--color-text);
	}
	.metric .time {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
	}
	.reason {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
		margin-left: 0.5rem;
		color: var(--color-danger);
		font-size: var(--text-xs);
		max-width: 16rem;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		vertical-align: middle;
	}
</style>
