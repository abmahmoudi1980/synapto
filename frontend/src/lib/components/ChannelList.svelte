<script lang="ts">
	import type { Channel } from '$lib/api';

	export let channels: Channel[];
	export let onRemove: (id: string) => void;

	function statusColor(status: string): string {
		switch (status) {
			case 'active':
				return '#16a34a';
			case 'inaccessible':
				return '#d97706';
			case 'banned':
				return '#dc2626';
			default:
				return '#6b7280';
		}
	}

	function formatDate(s: string | null): string {
		if (!s) return '—';
		try {
			return new Date(s).toLocaleString();
		} catch {
			return s;
		}
	}
</script>

<table class="channel-table">
	<thead>
		<tr>
			<th>Handle</th>
			<th>Name</th>
			<th>Status</th>
			<th>Last observed</th>
			<th></th>
		</tr>
	</thead>
	<tbody>
		{#each channels as ch (ch.id)}
			<tr>
				<td class="handle">@{ch.handle}</td>
				<td>{ch.display_name}</td>
				<td>
					<span class="badge" style="background:{statusColor(ch.status)}">
						{ch.status}
					</span>
				</td>
				<td class="muted">{formatDate(ch.last_observed_at)}</td>
				<td>
					<button on:click={() => onRemove(ch.id)} class="remove-btn">Remove</button>
				</td>
			</tr>
		{:else}
			<tr>
				<td colspan="5" class="empty">No channels selected. Add one above.</td>
			</tr>
		{/each}
	</tbody>
</table>

<style>
	.channel-table {
		width: 100%;
		border-collapse: collapse;
		font-size: 0.9rem;
	}
	.channel-table th {
		text-align: left;
		padding: 0.5rem 0.75rem;
		border-bottom: 2px solid #e5e7eb;
		color: #6b7280;
		font-weight: 600;
	}
	.channel-table td {
		padding: 0.5rem 0.75rem;
		border-bottom: 1px solid #f3f4f6;
	}
	.handle {
		font-family: monospace;
	}
	.muted {
		color: #6b7280;
	}
	.badge {
		display: inline-block;
		padding: 0.15rem 0.5rem;
		border-radius: 999px;
		color: #fff;
		font-size: 0.75rem;
		font-weight: 600;
	}
	.remove-btn {
		padding: 0.25rem 0.6rem;
		border: 1px solid #dc2626;
		background: transparent;
		color: #dc2626;
		border-radius: 4px;
		cursor: pointer;
		font-size: 0.8rem;
	}
	.remove-btn:hover {
		background: #dc2626;
		color: #fff;
	}
	.empty {
		text-align: center;
		color: #6b7280;
		padding: 1.5rem;
	}
</style>
