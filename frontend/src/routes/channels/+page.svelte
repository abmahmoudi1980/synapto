<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type Channel } from '$lib/api';
	import ChannelList from '$lib/components/ChannelList.svelte';

	let channels: Channel[] = [];
	let newHandle = '';
	let loading = false;
	let error = '';
	let success = '';

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
</script>

<svelte:head>
	<title>Channels — Synapto Admin</title>
</svelte:head>

<h1>Channels</h1>

<div class="add-form">
	<input
		type="text"
		bind:value={newHandle}
		placeholder="channel_handle (e.g. sample_news)"
		on:keydown={(e) => e.key === 'Enter' && addChannel()}
	/>
	<button on:click={addChannel} disabled={loading}>Add</button>
</div>

{#if error}
	<p class="error">{error}</p>
{/if}
{#if success}
	<p class="success">{success}</p>
{/if}

{#if loading}
	<p>Loading…</p>
{:else}
	<ChannelList {channels} onRemove={removeChannel} />
{/if}

<style>
	.add-form {
		display: flex;
		gap: 0.5rem;
		margin-bottom: 1rem;
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
