<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type CycleListEntry, type Post, type Settings } from '$lib/api';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import Icon from '$lib/components/Icon.svelte';

	let settings: Settings | null = null;
	let cycles: CycleListEntry[] = [];
	let posts: Post[] = [];
	let total = 0;
	let loading = false;
	let error = '';

	type StatusFilter = 'all' | 'succeeded' | 'degraded' | 'failed' | 'skipped_no_items';
	const filters: StatusFilter[] = ['all', 'succeeded', 'degraded', 'failed', 'skipped_no_items'];
	let statusFilter: StatusFilter = 'all';
	let limit = 50;
	let offset = 0;

	onMount(async () => {
		loading = true;
		error = '';
		try {
			const s = await api.getSettings();
			settings = s.settings;
			if (settings.delivery_mode === 'per_post') {
				const p = await api.listPosts({ status: 'sent', limit: 100 });
				posts = p.posts;
			} else {
				const c = await api.listCycles({ limit, offset });
				cycles = c.cycles;
				total = c.total;
			}
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	function setFilter(f: StatusFilter) {
		statusFilter = f;
		offset = 0;
	}

	$: isPerPost = settings?.delivery_mode === 'per_post';
	$: visible = statusFilter === 'all' ? cycles : cycles.filter((c) => c.status === statusFilter);
	$: counts = {
		all: total,
		succeeded: cycles.filter((c) => c.status === 'succeeded').length,
		degraded: cycles.filter((c) => c.status === 'degraded').length,
		failed: cycles.filter((c) => c.status === 'failed').length,
		skipped_no_items: cycles.filter((c) => c.status === 'skipped_no_items').length
	} as Record<StatusFilter, number>;

	function statusKind(s: CycleListEntry['status']): 'ok' | 'degraded' | 'failed' | 'skipped' {
		switch (s) {
			case 'succeeded':
				return 'ok';
			case 'degraded':
				return 'degraded';
			case 'failed':
				return 'failed';
			case 'skipped_no_items':
				return 'skipped';
			default:
				return 'skipped';
		}
	}

	function postStatusKind(s: Post['status']): 'ok' | 'degraded' | 'failed' | 'skipped' | 'neutral' {
		switch (s) {
			case 'sent':
				return 'ok';
			case 'send_failed':
				return 'failed';
			case 'summarized':
			case 'included_in_digest':
			case 'received':
				return 'neutral';
			case 'filtered_out':
			case 'dead':
				return 'skipped';
			default:
				return 'neutral';
		}
	}

	function formatDate(iso: string | undefined | null): string {
		if (!iso) return '—';
		try {
			return new Date(iso).toLocaleString();
		} catch {
			return iso;
		}
	}

	function pageOffset(delta: number) {
		const next = Math.max(0, offset + delta * limit);
		if (next !== offset) {
			offset = next;
			loadCycles();
		}
	}

	async function loadCycles() {
		loading = true;
		try {
			const c = await api.listCycles({ limit, offset });
			cycles = c.cycles;
			total = c.total;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	function truncateSummary(s: string, n = 120): string {
		if (s.length <= n) return s;
		return s.slice(0, n - 1) + '…';
	}
</script>

<svelte:head>
	<title>History — Synapto Admin</title>
</svelte:head>

<header class="page-head">
	<div>
		<p class="eyebrow">Audit</p>
		<h1>History</h1>
		<p class="lede">
			{#if isPerPost}
				Each row is one Telegram message that was sent to the subscriber chat. The
				queue is a per-post delivery record; failures are isolated per post and retried
				on the next cycle.
			{:else}
				Every scheduled cycle, what it produced, and what the operator should know.
				Cycles are stored forever; the most recent {limit} are shown.
			{/if}
		</p>
	</div>
	<div class="head-stat">
		<span class="stat-num">{isPerPost ? posts.length : total}</span>
		<span class="stat-label">{isPerPost ? 'sent posts' : 'total cycles'}</span>
	</div>
</header>

{#if error}
	<div class="alert danger" role="alert">
		<Icon name="alert" size={18} />
		<div>
			<strong>Couldn't load history.</strong>
			<span>{error}</span>
		</div>
	</div>
{/if}

{#if isPerPost}
	{#if loading && posts.length === 0}
		<div class="loading surface">
			<Icon name="spinner" size={18} />
			<span>Loading sent posts…</span>
		</div>
	{:else if posts.length === 0}
		<div class="empty surface">
			<Icon name="history" size={32} />
			<p>No sent posts yet. The first one will appear here after the next cycle.</p>
		</div>
	{:else}
		<div class="table-wrap surface">
			<table class="data-table" aria-label="Sent posts">
				<thead>
					<tr>
						<th scope="col">Sent at</th>
						<th scope="col">Channel</th>
						<th scope="col">Category</th>
						<th scope="col">Summary</th>
						<th scope="col" class="num-col">Attempts</th>
					</tr>
				</thead>
				<tbody>
					{#each posts as p (p.id)}
						<tr>
							<td class="mono">{formatDate(p.sent_at)}</td>
							<td>
								{#if p.channel_handle}
									<strong>@{p.channel_handle}</strong>
								{:else}
									—
								{/if}
							</td>
							<td>
								<StatusBadge
									kind={postStatusKind(p.status)}
									label={p.category_name || 'Uncategorized'}
								/>
							</td>
							<td>{truncateSummary(p.summary)}</td>
							<td class="num-col mono">{p.attempts}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
{:else}
	<div class="filter-bar" role="tablist" aria-label="Filter cycles by status">
		{#each filters as f (f)}
			<button
				type="button"
				role="tab"
				aria-selected={statusFilter === f}
				class="filter-chip"
				class:active={statusFilter === f}
				on:click={() => setFilter(f)}
			>
				{f.replace('_', ' ')}
				<span class="count">{counts[f]}</span>
			</button>
		{/each}
	</div>

	{#if loading && cycles.length === 0}
		<div class="loading surface">
			<Icon name="spinner" size={18} />
			<span>Loading cycles…</span>
		</div>
	{:else if visible.length === 0}
		<div class="empty surface">
			<Icon name="history" size={32} />
			<p>
				{#if total === 0}
					No cycles yet. The first one will appear here after the next interval.
				{:else}
					No cycles in this status.
				{/if}
			</p>
		</div>
	{:else}
		<div class="table-wrap surface">
			<table class="data-table" aria-label="Cycles">
				<thead>
					<tr>
						<th scope="col">Window</th>
						<th scope="col">Status</th>
						<th scope="col" class="num-col">Items</th>
						<th scope="col" class="num-col">Inputs</th>
						<th scope="col">Finished</th>
						<th scope="col" class="actions-col"><span class="sr-only">View</span></th>
					</tr>
				</thead>
				<tbody>
					{#each visible as c (c.id)}
						{@const skipped = c.status === 'skipped_no_items'}
						<tr>
							<td class="time">
								<strong>{formatDate(c.window_end)}</strong>
								<small class="muted">{formatDate(c.window_start)}</small>
							</td>
							<td>
								<StatusBadge
									kind={statusKind(c.status)}
									label={c.status.replace('_', ' ')}
								/>
							</td>
							<td class="num-col mono">{c.output_items}</td>
							<td class="num-col mono">{c.input_msg_count}</td>
							<td class="mono">{c.finished_at ? formatDate(c.finished_at) : '—'}</td>
							<td class="actions">
								{#if skipped}
									<span class="muted small">no items</span>
								{:else}
									<a class="btn-link" href="/history/{c.id}">
										View
										<Icon name="arrow-right" size={14} />
									</a>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>

		<nav class="pagination" aria-label="Pagination">
			<button class="btn-ghost" disabled={offset === 0} on:click={() => pageOffset(-1)}>
				<Icon name="arrow-left" size={14} />
				Newer
			</button>
			<span class="page-meta">
				Showing {offset + 1}–{offset + visible.length} of {total}
			</span>
			<button
				class="btn-ghost"
				disabled={offset + visible.length >= total}
				on:click={() => pageOffset(1)}
			>
				Older
				<Icon name="arrow-right" size={14} />
			</button>
		</nav>
	{/if}
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
	.alert strong {
		display: block;
		font-weight: 600;
	}
	.alert span {
		font-size: var(--text-sm);
		color: var(--color-text);
	}

	.head-stat {
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		padding: 0.5rem 0.875rem;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		box-shadow: var(--shadow-sm);
	}
	.stat-num {
		font-family: var(--font-mono);
		font-weight: 600;
		font-size: var(--text-lg);
		color: var(--color-text);
		line-height: 1;
	}
	.stat-label {
		font-size: 0.65rem;
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--color-text-subtle);
	}

	.filter-bar {
		display: flex;
		gap: 0.4rem;
		margin-bottom: var(--space-4);
		flex-wrap: wrap;
	}
	.filter-chip {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.4rem 0.75rem;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-pill);
		color: var(--color-text-muted);
		font-size: var(--text-xs);
		font-weight: 500;
		text-transform: lowercase;
		letter-spacing: 0.04em;
		cursor: pointer;
		transition:
			background var(--duration-base) var(--ease-out),
			color var(--duration-base) var(--ease-out),
			border-color var(--duration-base) var(--ease-out);
	}
	.filter-chip:hover {
		background: var(--color-surface-2);
		color: var(--color-text);
		border-color: var(--color-border-strong);
	}
	.filter-chip.active {
		background: var(--color-primary);
		color: #fff;
		border-color: var(--color-primary);
	}
	.filter-chip .count {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-width: 1.2rem;
		height: 1.2rem;
		padding: 0 0.3rem;
		background: var(--color-surface-2);
		color: var(--color-text-muted);
		border-radius: var(--radius-pill);
		font-family: var(--font-mono);
		font-size: 0.7rem;
	}
	.filter-chip.active .count {
		background: rgba(255, 255, 255, 0.2);
		color: #fff;
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
	.empty p {
		margin: 0;
		max-width: 50ch;
	}

	.table-wrap {
		overflow: hidden;
	}
	.data-table {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--text-sm);
	}
	.data-table th {
		text-align: left;
		padding: 0.625rem 0.875rem;
		background: var(--color-surface-2);
		border-bottom: 1px solid var(--color-border);
		color: var(--color-text-subtle);
		font-weight: 600;
		font-size: var(--text-xs);
		letter-spacing: 0.04em;
		text-transform: uppercase;
	}
	.data-table td {
		padding: 0.625rem 0.875rem;
		border-bottom: 1px solid var(--color-border);
		vertical-align: middle;
	}
	.data-table tbody tr:last-child td {
		border-bottom: none;
	}
	.data-table tbody tr {
		transition: background var(--duration-base) var(--ease-out);
	}
	.data-table tbody tr:hover {
		background: var(--color-surface-2);
	}
	.time {
		display: flex;
		flex-direction: column;
		gap: 0.1rem;
	}
	.time strong {
		font-size: var(--text-sm);
	}
	.time .muted {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
	}
	.num-col {
		text-align: right;
	}
	.mono {
		font-family: var(--font-mono);
	}
	.muted {
		color: var(--color-text-subtle);
	}
	.small {
		font-size: var(--text-xs);
	}
	.actions-col {
		width: 1%;
		white-space: nowrap;
	}
	.actions {
		text-align: right;
	}
	.btn-link {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		padding: 0.3rem 0.6rem;
		font-size: var(--text-xs);
		font-weight: 600;
		color: var(--color-primary);
		background: var(--color-primary-soft);
		border-radius: var(--radius-md);
		text-decoration: none;
		transition: background var(--duration-base) var(--ease-out);
	}
	.btn-link:hover {
		background: var(--color-primary);
		color: #fff;
		text-decoration: none;
	}
	.btn-link:focus-visible {
		outline: none;
		box-shadow: var(--focus-ring);
	}

	.pagination {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 0.5rem;
		margin-top: var(--space-3);
	}
	.btn-ghost {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		padding: 0.45rem 0.75rem;
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
		opacity: 0.4;
		cursor: not-allowed;
	}
	.page-meta {
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
		font-family: var(--font-mono);
	}

	:global(.sr-only) {
		position: absolute;
		width: 1px;
		height: 1px;
		padding: 0;
		margin: -1px;
		overflow: hidden;
		clip: rect(0, 0, 0, 0);
		white-space: nowrap;
		border: 0;
	}
</style>
