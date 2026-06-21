<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { api, type CycleDetail, type OpEvent } from '$lib/api';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import DigestViewer from '$lib/components/DigestViewer.svelte';
	import Icon from '$lib/components/Icon.svelte';

	let detail: CycleDetail | null = null;
	let events: OpEvent[] = [];
	let loading = false;
	let error = '';
	let notFound = false;
	let cycleId = '';

	$: {
		cycleId = $page.params.id ?? '';
		if (cycleId) load();
	}

	onMount(() => {
		// initial load is triggered by the reactive $: above
	});

	async function load() {
		loading = true;
		error = '';
		notFound = false;
		try {
			detail = await api.getCycle(cycleId);
			// Also pull events for this cycle.
			const ev = await api.listEvents({ limit: 50 });
			events = ev.events.filter((e) => e.cycle_id === cycleId);
		} catch (e) {
			const msg = (e as Error).message;
			if (msg.includes('cycle not found') || msg.includes('HTTP 404')) {
				notFound = true;
			} else if (msg.includes('digest not available') || msg.includes('HTTP 410')) {
				// Skipped or failed cycle: load just the cycle, not the digest.
				notFound = false;
				await loadCycleOnly();
			} else {
				error = msg;
			}
		} finally {
			loading = false;
		}
	}

	async function loadCycleOnly() {
		// For skipped cycles the digest is unavailable; we still want to
		// show the cycle row. Hit the list endpoint to get a summary.
		try {
			const res = await api.listCycles({ limit: 200 });
			const c = res.cycles.find((x) => x.id === cycleId);
			if (c) {
				detail = {
					cycle: c,
					digest: {
						id: '',
						rendered_text: '',
						degraded: false,
						telegram_msg_id: null,
						sent_at: '',
						send_status: 'ok'
					},
					items_by_category: []
				};
			} else {
				notFound = true;
			}
		} catch (e) {
			error = (e as Error).message;
		}
	}

	function formatDate(iso: string | null | undefined): string {
		if (!iso) return '—';
		try {
			return new Date(iso).toLocaleString();
		} catch {
			return iso;
		}
	}

	function levelKind(l: string): 'info' | 'warning' | 'danger' {
		if (l === 'error') return 'danger';
		if (l === 'warn') return 'warning';
		return 'info';
	}

	function statusKind(s: string): 'ok' | 'degraded' | 'failed' | 'skipped' {
		switch (s) {
			case 'succeeded':
				return 'ok';
			case 'degraded':
				return 'degraded';
			case 'failed':
				return 'failed';
			default:
				return 'skipped';
		}
	}
</script>

<svelte:head>
	<title>Cycle {cycleId.slice(0, 8)} — Synapto Admin</title>
</svelte:head>

<nav class="breadcrumb" aria-label="Breadcrumb">
	<a href="/history">
		<Icon name="arrow-left" size={14} />
		All cycles
	</a>
</nav>

{#if loading}
	<div class="loading surface">
		<Icon name="spinner" size={18} />
		<span>Loading cycle…</span>
	</div>
{:else if notFound}
	<div class="empty surface">
		<Icon name="history" size={32} />
		<h2>Cycle not found</h2>
		<p>
			No cycle with id <code>{cycleId}</code> exists in the local database. It may have been removed,
			or the link is wrong.
		</p>
	</div>
{:else if error}
	<div class="alert danger" role="alert">
		<Icon name="alert" size={18} />
		<div>
			<strong>Couldn't load this cycle.</strong>
			<span>{error}</span>
		</div>
	</div>
{:else if detail}
	<header class="page-head">
		<div>
			<p class="eyebrow">Cycle · {detail.cycle.id.slice(0, 8)}</p>
			<h1>
				Window {formatDate(detail.cycle.window_start)} → {formatDate(
					detail.cycle.window_end
				)}
			</h1>
			<p class="lede">
				Started {formatDate(detail.cycle.started_at)} · finished
				{formatDate(detail.cycle.finished_at)}
			</p>
		</div>
		<StatusBadge
			kind={statusKind(detail.cycle.status)}
			label={detail.cycle.status.replace('_', ' ')}
			size="md"
		/>
	</header>

	{#if detail.cycle.status === 'skipped_no_items'}
		<section class="surface callout" aria-label="No items">
			<header>
				<Icon name="info" size={18} />
				<h2>No digest produced</h2>
			</header>
			<p>
				This cycle ran and found no new items in any channel. The service recorded the empty
				window so the operator can see the cadence is working. No message was sent to the
				subscriber.
			</p>
		</section>
	{:else}
		<DigestViewer digest={detail.digest} itemsByCategory={detail.items_by_category} />
	{/if}

	{#if events.length > 0}
		<section class="surface section" aria-labelledby="events-heading">
			<h2 id="events-heading">Operational events for this cycle</h2>
			<ul class="event-list">
				{#each events as ev (ev.id)}
					<li class="event">
						<span class="event-time">{formatDate(ev.occurred_at)}</span>
						<StatusBadge kind={levelKind(ev.level)} label={ev.level} />
						<span class="event-kind">{ev.kind}</span>
						<span class="event-msg">{ev.message}</span>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
{/if}

<style>
	.breadcrumb {
		margin-bottom: var(--space-3);
	}
	.breadcrumb a {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		font-size: var(--text-sm);
		color: var(--color-text-muted);
	}
	.breadcrumb a:hover {
		color: var(--color-primary);
	}

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
		font-family: var(--font-mono);
	}
	.lede {
		color: var(--color-text-muted);
		margin: 0;
		max-width: 60ch;
		font-size: var(--text-sm);
	}

	.loading,
	.empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.75rem;
		padding: var(--space-8);
		color: var(--color-text-muted);
		font-size: var(--text-sm);
		text-align: center;
	}
	.empty h2 {
		font-size: var(--text-xl);
		margin: 0;
		color: var(--color-text);
	}
	.empty p {
		margin: 0;
		max-width: 50ch;
	}
	.empty code {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		background: var(--color-surface-2);
		padding: 0.1rem 0.4rem;
		border-radius: var(--radius-sm);
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
	.alert strong {
		display: block;
		font-weight: 600;
	}
	.alert span {
		font-size: var(--text-sm);
		color: var(--color-text);
	}

	.callout {
		padding: var(--space-4) var(--space-5);
	}
	.callout header {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		color: var(--color-text);
		margin-bottom: 0.5rem;
	}
	.callout h2 {
		font-size: var(--text-lg);
		margin: 0;
	}
	.callout p {
		margin: 0;
		font-size: var(--text-sm);
		color: var(--color-text-muted);
	}

	.section {
		padding: var(--space-4) var(--space-5);
		margin-top: var(--space-4);
	}
	.section h2 {
		font-size: var(--text-lg);
		margin: 0 0 var(--space-3);
	}

	.event-list {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.event {
		display: grid;
		grid-template-columns: auto auto 1fr 1fr;
		gap: 0.5rem 1rem;
		align-items: center;
		padding: 0.4rem 0;
		border-bottom: 1px solid var(--color-border);
		font-size: var(--text-sm);
	}
	.event:last-child {
		border-bottom: none;
	}
	.event-time {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
		white-space: nowrap;
	}
	.event-kind {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--color-text);
	}
	.event-msg {
		font-size: var(--text-sm);
		color: var(--color-text-muted);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
</style>
