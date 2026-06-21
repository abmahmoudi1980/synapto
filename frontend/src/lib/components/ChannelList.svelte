<script lang="ts">
	import type { Channel } from '$lib/api';
	import StatusBadge from './StatusBadge.svelte';
	import Icon from './Icon.svelte';

	export let channels: Channel[];
	export let onRemove: (id: string) => void;

	let confirmId: string | null = null;

	function statusKind(status: Channel['status']): 'active' | 'inaccessible' | 'banned' {
		return status;
	}

	function formatDate(s: string | null): string {
		if (!s) return '—';
		try {
			return new Date(s).toLocaleString();
		} catch {
			return s;
		}
	}

	function askRemove(id: string) {
		confirmId = id;
	}
	function cancelRemove() {
		confirmId = null;
	}
	function doRemove(id: string) {
		confirmId = null;
		onRemove(id);
	}
</script>

<div class="table-wrap surface">
	<table class="data-table" aria-label="Channels">
		<thead>
			<tr>
				<th scope="col">Handle</th>
				<th scope="col">Name</th>
				<th scope="col">Status</th>
				<th scope="col">Last observed</th>
				<th scope="col" class="actions-col"><span class="sr-only">Actions</span></th>
			</tr>
		</thead>
		<tbody>
			{#each channels as ch (ch.id)}
				<tr>
					<td class="handle">@{ch.handle}</td>
					<td class="name">{ch.display_name}</td>
					<td>
						<StatusBadge kind={statusKind(ch.status)} label={ch.status} />
						{#if ch.last_error}
							<span class="last-error" title={ch.last_error}>
								<Icon name="warning" size={12} />
								{ch.last_error}
							</span>
						{/if}
					</td>
					<td class="muted">{formatDate(ch.last_observed_at)}</td>
					<td class="actions">
						{#if confirmId === ch.id}
							<span class="confirm-row">
								<span class="confirm-text">Remove?</span>
								<button
									class="btn-danger btn-sm"
									on:click={() => doRemove(ch.id)}
									aria-label="Confirm remove @{ch.handle}"
								>
									<Icon name="check" size={14} strokeWidth={2.5} />
									Yes
								</button>
								<button
									class="btn-ghost btn-sm"
									on:click={cancelRemove}
									aria-label="Cancel remove"
								>
									<Icon name="x" size={14} strokeWidth={2.5} />
									No
								</button>
							</span>
						{:else}
							<button
								class="btn-danger btn-sm"
								on:click={() => askRemove(ch.id)}
								aria-label="Remove channel @{ch.handle}"
							>
								<Icon name="trash" size={14} />
								Remove
							</button>
						{/if}
					</td>
				</tr>
			{:else}
				<tr>
					<td colspan="5" class="empty">
						<div class="empty-state">
							<Icon name="channel" size={24} />
							<p>No channels yet. Add one above to start receiving digests.</p>
						</div>
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>

<style>
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
	.handle {
		font-family: var(--font-mono);
		font-weight: 500;
		color: var(--color-text);
	}
	.name {
		color: var(--color-text);
	}
	.muted {
		color: var(--color-text-muted);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
	}
	.last-error {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
		margin-left: 0.5rem;
		font-size: var(--text-xs);
		color: var(--color-danger);
		max-width: 14rem;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		vertical-align: middle;
	}
	.actions-col {
		width: 1%;
		white-space: nowrap;
	}
	.actions {
		text-align: right;
		white-space: nowrap;
	}
	.confirm-row {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
	}
	.confirm-text {
		font-size: var(--text-sm);
		color: var(--color-text-muted);
	}

	.btn-sm {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		padding: 0.3rem 0.6rem;
		font-size: var(--text-xs);
		font-weight: 500;
		border-radius: var(--radius-md);
		border: 1px solid transparent;
		cursor: pointer;
		transition:
			background var(--duration-base) var(--ease-out),
			color var(--duration-base) var(--ease-out),
			border-color var(--duration-base) var(--ease-out);
	}
	.btn-danger {
		background: transparent;
		color: var(--color-danger);
		border-color: var(--color-danger-soft);
	}
	.btn-danger:hover {
		background: var(--color-danger);
		color: #fff;
	}
	.btn-ghost {
		background: transparent;
		color: var(--color-text-muted);
		border-color: var(--color-border);
	}
	.btn-ghost:hover {
		background: var(--color-surface-2);
		color: var(--color-text);
	}

	.empty {
		padding: 0;
	}
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 0.5rem;
		padding: var(--space-8);
		color: var(--color-text-subtle);
		text-align: center;
	}
	.empty-state p {
		margin: 0;
		font-size: var(--text-sm);
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
