<script lang="ts">
	import { createEventDispatcher } from 'svelte';
	import type { Category } from '$lib/api';
	import StatusBadge from './StatusBadge.svelte';
	import Icon from './Icon.svelte';

	export let categories: Category[];

	const dispatch = createEventDispatcher<{
		rename: { id: string; name: string };
		remove: { id: string; name: string };
	}>();

	let editingId: string | null = null;
	let editingName = '';
	let confirmId: string | null = null;

	function startEdit(c: Category) {
		editingId = c.id;
		editingName = c.name;
	}

	function commitEdit() {
		if (editingId == null) return;
		const id = editingId;
		const name = editingName.trim();
		const original = categories.find((c) => c.id === id);
		editingId = null;
		editingName = '';
		if (!original) return;
		if (!name || name === original.name) return;
		dispatch('rename', { id, name });
	}

	function cancelEdit() {
		editingId = null;
		editingName = '';
	}

	function askRemove(c: Category) {
		confirmId = c.id;
	}

	function doRemove() {
		if (confirmId == null) return;
		const id = confirmId;
		const original = categories.find((c) => c.id === id);
		confirmId = null;
		if (!original) return;
		dispatch('remove', { id, name: original.name });
	}

	function cancelRemove() {
		confirmId = null;
	}

	function onEditKey(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			commitEdit();
		} else if (e.key === 'Escape') {
			e.preventDefault();
			cancelEdit();
		}
	}
</script>

<div class="table-wrap surface">
	<table class="data-table" aria-label="Categories">
		<thead>
			<tr>
				<th scope="col">Name</th>
				<th scope="col">Type</th>
				<th scope="col">Ordering</th>
				<th scope="col" class="actions-col"><span class="sr-only">Actions</span></th>
			</tr>
		</thead>
		<tbody>
			{#each categories as c (c.id)}
				<tr>
					<td class="name">
						{#if editingId === c.id}
							<input
								type="text"
								class="inline-edit"
								bind:value={editingName}
								on:blur={commitEdit}
								on:keydown={onEditKey}
								maxlength="40"
								aria-label="Rename category"
							/>
						{:else}
							<button
								type="button"
								class="name-btn"
								on:click={() => startEdit(c)}
								aria-label="Rename {c.name}"
							>
								{c.name}
								<span class="edit-hint" aria-hidden="true">
									<Icon name="pencil" size={12} />
								</span>
							</button>
						{/if}
					</td>
					<td>
						<StatusBadge
							kind={c.is_default ? 'default' : 'custom'}
							label={c.is_default ? 'default' : 'custom'}
						/>
					</td>
					<td class="ordering">#{c.ordering}</td>
					<td class="actions">
						{#if confirmId === c.id}
							<span class="confirm-row">
								<span class="confirm-text">Remove "{c.name}"?</span>
								<button
									class="btn-danger btn-sm"
									on:click={doRemove}
									aria-label="Confirm remove {c.name}"
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
								disabled={c.is_default}
								on:click={() => askRemove(c)}
								title={c.is_default
									? 'Default categories cannot be removed; rename them instead.'
									: 'Remove this category'}
								aria-label="Remove {c.name}"
							>
								<Icon name="trash" size={14} />
								Remove
							</button>
						{/if}
					</td>
				</tr>
			{:else}
				<tr>
					<td colspan="4" class="empty">
						<div class="empty-state">
							<Icon name="tag" size={24} />
							<p>No categories yet. Add one above to start grouping digest items.</p>
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

	.name-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		background: none;
		border: none;
		padding: 0.2rem 0.4rem;
		margin: -0.2rem -0.4rem;
		border-radius: var(--radius-sm);
		font: inherit;
		color: var(--color-text);
		cursor: pointer;
		transition: background var(--duration-base) var(--ease-out);
	}
	.name-btn:hover {
		background: var(--color-primary-soft);
		color: var(--color-primary-soft-text);
	}
	.name-btn:focus-visible {
		outline: none;
		box-shadow: var(--focus-ring);
	}
	.edit-hint {
		opacity: 0;
		transition: opacity var(--duration-base) var(--ease-out);
		color: var(--color-text-subtle);
	}
	.name-btn:hover .edit-hint,
	.name-btn:focus-visible .edit-hint {
		opacity: 1;
	}
	.inline-edit {
		max-width: 18rem;
		padding: 0.3rem 0.5rem;
	}

	.ordering {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
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
	.btn-danger:hover:not(:disabled) {
		background: var(--color-danger);
		color: #fff;
	}
	.btn-danger:disabled {
		opacity: 0.4;
		cursor: not-allowed;
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
