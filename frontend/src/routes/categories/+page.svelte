<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type Category } from '$lib/api';
	import CategoryList from '$lib/components/CategoryList.svelte';
	import Icon from '$lib/components/Icon.svelte';

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

	$: defaultCount = categories.filter((c) => c.is_default).length;
	$: customCount = categories.length - defaultCount;
</script>

<svelte:head>
	<title>Categories — Synapto Admin</title>
</svelte:head>

<header class="page-head">
	<div>
		<p class="eyebrow">Taxonomy</p>
		<h1>Categories</h1>
		<p class="lede">
			Group digest items by category. Defaults ship with the service; custom ones can be added
			and removed freely. The next digest cycle uses the current names.
		</p>
	</div>
	<div class="stat-strip" aria-hidden="true">
		<div class="stat">
			<span class="stat-num">{defaultCount}</span>
			<span class="stat-label">default</span>
		</div>
		<div class="stat-sep"></div>
		<div class="stat">
			<span class="stat-num">{customCount}</span>
			<span class="stat-label">custom</span>
		</div>
	</div>
</header>

<form class="add-card surface" on:submit|preventDefault={addCategory} aria-label="Add category">
	<label for="new-name" class="sr-only">Category name</label>
	<div class="add-row">
		<span class="add-icon" aria-hidden="true">
			<Icon name="plus" size={16} strokeWidth={2.5} />
		</span>
		<input
			id="new-name"
			type="text"
			bind:value={newName}
			placeholder="New category name (e.g. AI & ML)"
			maxlength="40"
			autocomplete="off"
		/>
		<button type="submit" class="btn-primary" disabled={loading || !newName.trim()}>
			Add category
		</button>
	</div>
	<p class="hint">Max 40 characters. Names are case-insensitively unique.</p>
</form>

{#if error}
	<div class="alert danger" role="alert">
		<Icon name="alert" size={18} />
		<div>
			<strong>Couldn't add or update category.</strong>
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

{#if loading && categories.length === 0}
	<div class="loading surface">
		<Icon name="spinner" size={18} />
		<span>Loading categories…</span>
	</div>
{:else}
	<CategoryList {categories} on:rename={renameCategory} on:remove={removeCategory} />
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
		max-width: 56ch;
		font-size: var(--text-sm);
	}

	.stat-strip {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.5rem 0.875rem;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		box-shadow: var(--shadow-sm);
	}
	.stat {
		display: flex;
		flex-direction: column;
		line-height: 1.1;
	}
	.stat-num {
		font-family: var(--font-mono);
		font-weight: 600;
		font-size: var(--text-lg);
		color: var(--color-text);
	}
	.stat-label {
		font-size: 0.65rem;
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--color-text-subtle);
	}
	.stat-sep {
		width: 1px;
		height: 24px;
		background: var(--color-border);
	}

	.add-card {
		padding: var(--space-3);
		margin-bottom: var(--space-3);
	}
	.add-row {
		display: flex;
		align-items: center;
		gap: 0;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-surface);
		overflow: hidden;
		transition: border-color var(--duration-base) var(--ease-out);
	}
	.add-row:focus-within {
		border-color: var(--color-primary);
		box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.18);
	}
	.add-icon {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		padding: 0 var(--space-2) 0 var(--space-3);
		color: var(--color-text-subtle);
	}
	.add-row input {
		flex: 1;
		border: none;
		background: transparent;
		padding: 0.55rem 0.5rem;
	}
	.add-row input:focus {
		box-shadow: none;
		border: none;
	}
	.btn-primary {
		padding: 0.55rem 1rem;
		background: var(--color-primary);
		color: #fff;
		border: none;
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
	.hint {
		margin: 0.4rem 0 0;
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
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
