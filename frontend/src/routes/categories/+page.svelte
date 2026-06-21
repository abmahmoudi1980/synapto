<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type Category } from '$lib/api';
	import CategoryList from '$lib/components/CategoryList.svelte';

	let categories: Category[] = [];
	let newName = '';
	let loading = false;
	let error = '';
	let success = '';

	onMount(loadCategories);

	async function loadCategories() {
		loading = true;
		error = '';
		try {
			const res = await api.listCategories();
			categories = res.categories;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	async function addCategory() {
		error = '';
		success = '';
		const name = newName.trim();
		if (!name) {
			error = 'Category name must not be empty';
			return;
		}
		try {
			const res = await api.addCategory(name);
			categories = [...categories, res.category].sort((a, b) => {
				if (a.ordering !== b.ordering) return a.ordering - b.ordering;
				return a.name.localeCompare(b.name);
			});
			newName = '';
			success = `Added "${res.category.name}"`;
		} catch (e) {
			error = (e as Error).message;
		}
	}

	async function renameCategory(e: CustomEvent<{ id: string; name: string }>) {
		const { id, name } = e.detail;
		error = '';
		success = '';
		try {
			const res = await api.renameCategory(id, name);
			categories = categories
				.map((c) => (c.id === id ? res.category : c))
				.sort((a, b) => {
					if (a.ordering !== b.ordering) return a.ordering - b.ordering;
					return a.name.localeCompare(b.name);
				});
			success = `Renamed to "${res.category.name}"`;
		} catch (e) {
			error = (e as Error).message;
			await loadCategories();
		}
	}

	async function removeCategory(e: CustomEvent<{ id: string; name: string }>) {
		const { id, name } = e.detail;
		error = '';
		success = '';
		try {
			await api.removeCategory(id);
			categories = categories.filter((c) => c.id !== id);
			success = `Removed "${name}"`;
		} catch (e) {
			error = (e as Error).message;
		}
	}
</script>

<svelte:head>
	<title>Categories — Synapto Admin</title>
</svelte:head>

<h1>Categories</h1>

<p class="hint">
	Group digest items by category. Default categories ship with the service and can be renamed but
	not removed. Custom categories can be added freely and removed at any time. The next digest
	cycle uses the current names.
</p>

<div class="add-form">
	<input
		type="text"
		bind:value={newName}
		placeholder="New category name (e.g. AI & ML)"
		maxlength="40"
		on:keydown={(e) => e.key === 'Enter' && addCategory()}
	/>
	<button on:click={addCategory} disabled={loading}>Add</button>
</div>

{#if error}
	<p class="error">{error}</p>
{/if}
{#if success}
	<p class="success">{success}</p>
{/if}

{#if loading && categories.length === 0}
	<p>Loading…</p>
{:else}
	<CategoryList {categories} on:rename={renameCategory} on:remove={removeCategory} />
{/if}

<style>
	.hint {
		color: #4b5563;
		font-size: 0.9rem;
		max-width: 60ch;
	}
	.add-form {
		display: flex;
		gap: 0.5rem;
		margin: 1rem 0;
	}
	.add-form input {
		flex: 1;
		padding: 0.5rem 0.75rem;
		border: 1px solid #d1d5db;
		border-radius: 4px;
		font-size: 0.9rem;
	}
	.add-form button {
		padding: 0.5rem 1.25rem;
		background: #1f2933;
		color: #fff;
		border: none;
		border-radius: 4px;
		cursor: pointer;
	}
	.add-form button:disabled {
		opacity: 0.5;
	}
	.error {
		color: #dc2626;
		font-size: 0.85rem;
	}
	.success {
		color: #16a34a;
		font-size: 0.85rem;
	}
</style>
