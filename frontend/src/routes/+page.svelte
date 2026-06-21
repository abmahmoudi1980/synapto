<script lang="ts">
	import { onMount } from 'svelte';

	// Placeholder overview page. Real content arrives in User Story 4 (T049).
	let health: { status: string; version: string } | null = null;

	onMount(async () => {
		try {
			const res = await fetch('/api/health');
			health = await res.json();
		} catch {
			health = null;
		}
	});
</script>

<svelte:head>
	<title>Synapto Admin — Overview</title>
</svelte:head>

<h1>Synapto Admin</h1>
<p>Telegram News Digest Assistant — admin panel.</p>

{#if health}
	<p>Status: <strong>{health.status}</strong> · Version: {health.version}</p>
{:else}
	<p>Service unreachable. Start the backend and refresh.</p>
{/if}
