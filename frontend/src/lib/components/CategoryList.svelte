<script lang="ts">
	import { createEventDispatcher } from 'svelte';
	import type { Category } from '$lib/api';

	export let categories: Category[];

	const dispatch = createEventDispatcher<{
		rename: { id: string; name: string };
		remove: { id: string; name: string };
	}>();

	let editingId: string | null = null;
	let editingName = '';
	let confirmRemoveId: string | null = null;

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

	function startRemove(c: Category) {
		confirmRemoveId = c.id;
	}

	function confirmRemove() {
		if (confirmRemoveId == null) return;
		const id = confirmRemoveId;
		const original = categories.find((c) => c.id === id);
		confirmRemoveId = null;
		if (!original) return;
		dispatch('remove', { id, name: original.name });
	}

	function cancelRemove() {
		confirmRemoveId = null;
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

<table class="category-table">
	<thead>
		<tr>
			<th>Name</th>
			<th>Type</th>
			<th></th>
		</tr>
	</thead>
	<tbody>
		{#each categories as c (c.id)}
			<tr>
				<td class="name">
					{#if editingId === c.id}
						<input
							type="text"
							bind:value={editingName}
							on:blur={commitEdit}
							on:keydown={onEditKey}
							maxlength="40"
						/>
					{:else}
						<span
							class="editable"
							role="button"
							tabindex="0"
							on:click={() => startEdit(c)}
							on:keydown={(e) => (e.key === 'Enter' || e.key === ' ') && startEdit(c)}
							title="Click to rename"
						>
							{c.name}
						</span>
					{/if}
				</td>
				<td>
					{#if c.is_default}
						<span class="badge default">default</span>
					{:else}
						<span class="badge custom">custom</span>
					{/if}
				</td>
				<td>
					{#if confirmRemoveId === c.id}
						<span class="confirm">
							Remove "{c.name}"?
							<button class="danger small" on:click={confirmRemove}>Yes</button>
							<button class="small" on:click={cancelRemove}>No</button>
						</span>
					{:else}
						<button
							class="remove-btn"
							disabled={c.is_default}
							title={c.is_default
								? 'Default categories cannot be removed'
								: 'Remove this category'}
							on:click={() => startRemove(c)}
						>
							Remove
						</button>
					{/if}
				</td>
			</tr>
		{:else}
			<tr>
				<td colspan="3" class="empty">No categories yet.</td>
			</tr>
		{/each}
	</tbody>
</table>

<style>
	.category-table {
		width: 100%;
		border-collapse: collapse;
		font-size: 0.9rem;
	}
	.category-table th {
		text-align: left;
		padding: 0.5rem 0.75rem;
		border-bottom: 2px solid #e5e7eb;
		color: #6b7280;
		font-weight: 600;
	}
	.category-table td {
		padding: 0.5rem 0.75rem;
		border-bottom: 1px solid #f3f4f6;
		vertical-align: middle;
	}
	.editable {
		cursor: pointer;
		padding: 0.1rem 0.25rem;
		border-radius: 4px;
	}
	.editable:hover {
		background: #f3f4f6;
	}
	.name input {
		padding: 0.25rem 0.5rem;
		border: 1px solid #2563eb;
		border-radius: 4px;
		font-size: 0.9rem;
		font-family: inherit;
	}
	.badge {
		display: inline-block;
		padding: 0.15rem 0.5rem;
		border-radius: 999px;
		color: #fff;
		font-size: 0.7rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}
	.badge.default {
		background: #6b7280;
	}
	.badge.custom {
		background: #2563eb;
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
	.remove-btn:hover:not(:disabled) {
		background: #dc2626;
		color: #fff;
	}
	.remove-btn:disabled {
		opacity: 0.4;
		cursor: not-allowed;
	}
	.confirm {
		display: inline-flex;
		gap: 0.4rem;
		align-items: center;
		font-size: 0.85rem;
	}
	.confirm .danger {
		border-color: #dc2626;
		color: #dc2626;
	}
	.confirm .danger:hover {
		background: #dc2626;
		color: #fff;
	}
	.small {
		padding: 0.2rem 0.5rem;
		border: 1px solid #6b7280;
		background: transparent;
		color: #6b7280;
		border-radius: 4px;
		cursor: pointer;
		font-size: 0.8rem;
	}
	.small:hover {
		background: #6b7280;
		color: #fff;
	}
	.empty {
		text-align: center;
		color: #6b7280;
		padding: 1.5rem;
	}
</style>
