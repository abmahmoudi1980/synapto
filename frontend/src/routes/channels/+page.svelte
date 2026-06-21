<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type Channel } from '$lib/api';
	import ChannelList from '$lib/components/ChannelList.svelte';
	import Icon from '$lib/components/Icon.svelte';

	let channels: Channel[] = [];
	let newHandle = '';
	let loading = false;
	let error = '';
	let success = '';
	type Filter = 'all' | 'active' | 'inaccessible' | 'banned';
	const filters: Filter[] = ['all', 'active', 'inaccessible', 'banned'];
	let filter: Filter = 'all';

	onMount(loadChannels);

	async function loadChannels() {
		loading = true;
		error = '';
		try {
			const res = await api.listChannels();
			channels = res.channels;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	async function addChannel() {
		error = '';
		success = '';
		const handle = newHandle.trim().replace(/^@/, '');
		if (!handle) {
			error = 'Handle must not be empty';
			return;
		}
		try {
			const res = await api.addChannel(handle);
			channels = [...channels, res.channel].sort((a, b) => a.handle.localeCompare(b.handle));
			newHandle = '';
			success = `Added @${res.channel.handle}`;
		} catch (e) {
			error = (e as Error).message;
		}
	}

	async function removeChannel(id: string) {
		error = '';
		success = '';
		try {
			await api.removeChannel(id);
			channels = channels.filter((c) => c.id !== id);
			success = 'Channel removed';
		} catch (e) {
			error = (e as Error).message;
		}
	}

	$: visible = filter === 'all' ? channels : channels.filter((c) => c.status === filter);
	$: counts = {
		all: channels.length,
		active: channels.filter((c) => c.status === 'active').length,
		inaccessible: channels.filter((c) => c.status === 'inaccessible').length,
		banned: channels.filter((c) => c.status === 'banned').length
	} as Record<Filter, number>;
</script>

<svelte:head>
	<title>Channels — Synapto Admin</title>
</svelte:head>

<header class="page-head">
	<div>
		<p class="eyebrow">Sources</p>
		<h1>Channels</h1>
		<p class="lede">Telegram channels the assistant will fetch new posts from.</p>
	</div>
</header>

<form class="add-card surface" on:submit|preventDefault={addChannel} aria-label="Add channel">
	<label for="new-handle" class="sr-only">Channel handle</label>
	<div class="add-row">
		<span class="add-icon" aria-hidden="true">
			<Icon name="plus" size={16} strokeWidth={2.5} />
		</span>
		<span class="add-prefix" aria-hidden="true">@</span>
		<input
			id="new-handle"
			type="text"
			bind:value={newHandle}
			placeholder="channel_handle (e.g. sample_news)"
			autocomplete="off"
			spellcheck="false"
		/>
		<button type="submit" class="btn-primary" disabled={loading || !newHandle.trim()}>
			Add channel
		</button>
	</div>
</form>

{#if error}
	<div class="alert danger" role="alert">
		<Icon name="alert" size={18} />
		<div>
			<strong>Couldn't add channel.</strong>
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

<div class="filter-bar" role="tablist" aria-label="Filter by status">
	{#each filters as f (f)}
		<button
			type="button"
			role="tab"
			aria-selected={filter === f}
			class="filter-chip"
			class:active={filter === f}
			on:click={() => (filter = f)}
		>
			{f}
			<span class="count">{counts[f]}</span>
		</button>
	{/each}
</div>

{#if loading && channels.length === 0}
	<div class="loading surface">
		<Icon name="spinner" size={18} />
		<span>Loading channels…</span>
	</div>
{:else}
	<ChannelList channels={visible} onRemove={removeChannel} />
{/if}

<style>
	.page-head {
		margin-bottom: var(--space-5);
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

	.add-card {
		padding: var(--space-3);
		margin-bottom: var(--space-4);
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
	.add-prefix {
		font-family: var(--font-mono);
		color: var(--color-text-subtle);
		padding-right: 0.25rem;
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
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
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
		text-transform: uppercase;
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
