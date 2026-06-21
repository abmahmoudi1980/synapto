<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import Icon from '$lib/components/Icon.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';

	let password = '';
	let loading = false;
	let error = '';
	let authRequired = true;
	let checking = true;

	onMount(async () => {
		try {
			const status = await api.getAuthStatus();
			authRequired = status.auth_required;
			// If auth is disabled or we're already authenticated, skip
			// the login page.
			if (!status.auth_required || status.authenticated) {
				await goto('/');
				return;
			}
		} catch {
			// If status check fails, assume auth is required and show the form.
		} finally {
			checking = false;
		}
	});

	async function submit() {
		if (!password) return;
		loading = true;
		error = '';
		try {
			const res = await api.login(password);
			if (res.authenticated) {
				await goto('/');
			}
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			submit();
		}
	}
</script>

<svelte:head>
	<title>Sign in — Synapto Admin</title>
</svelte:head>

<div class="login-page">
	<article class="login-card surface">
		<header class="login-head">
			<span class="brand-mark" aria-hidden="true">
				<Icon name="logo" size={28} strokeWidth={1.75} />
			</span>
			<h1>Synapto Admin</h1>
			<p class="lede">Sign in to manage channels, categories, and history.</p>
		</header>

		{#if checking}
			<div class="state">
				<Icon name="spinner" size={18} />
				<span>Checking session…</span>
			</div>
		{:else if !authRequired}
			<div class="state info">
				<StatusBadge kind="neutral" label="auth disabled" />
				<p>Admin auth is not configured. Redirecting to the panel…</p>
			</div>
		{:else}
			<form on:submit|preventDefault={submit} class="login-form">
				<label class="field">
					<span class="field-label">Admin password</span>
					<input
						type="password"
						bind:value={password}
						on:keydown={onKeydown}
						autocomplete="current-password"
						placeholder="••••••••"
						disabled={loading}
						required
					/>
				</label>

				{#if error}
					<div class="error" role="alert">
						<Icon name="alert" size={16} />
						<span>{error}</span>
					</div>
				{/if}

				<button type="submit" class="btn-primary" disabled={loading || !password}>
					{#if loading}
						<Icon name="spinner" size={14} strokeWidth={2.5} />
						<span>Signing in…</span>
					{:else}
						<Icon name="arrow-right" size={14} />
						<span>Sign in</span>
					{/if}
				</button>
			</form>
		{/if}
	</article>
</div>

<style>
	.login-page {
		display: flex;
		align-items: center;
		justify-content: center;
		min-height: 100vh;
		padding: var(--space-6);
		background: var(--color-bg);
	}
	.login-card {
		width: 100%;
		max-width: 420px;
		padding: var(--space-6) var(--space-6);
	}
	.login-head {
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;
		gap: 0.5rem;
		margin-bottom: var(--space-5);
	}
	.brand-mark {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 48px;
		height: 48px;
		border-radius: var(--radius-md);
		background: var(--color-primary-soft);
		color: var(--color-primary);
	}
	.login-head h1 {
		font-size: var(--text-xl);
		margin: 0;
	}
	.lede {
		margin: 0;
		font-size: var(--text-sm);
		color: var(--color-text-muted);
		max-width: 32ch;
	}

	.state {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 0.5rem;
		padding: var(--space-4) 0;
		font-size: var(--text-sm);
		color: var(--color-text-muted);
	}
	.state.info {
		flex-direction: column;
		gap: 0.75rem;
		text-align: center;
	}
	.state p {
		margin: 0;
	}

	.login-form {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.field {
		display: flex;
		flex-direction: column;
		gap: 0.4rem;
	}
	.field-label {
		font-size: var(--text-sm);
		font-weight: 500;
		color: var(--color-text);
	}

	.error {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.5rem 0.75rem;
		background: var(--color-danger-soft);
		color: var(--color-danger);
		border: 1px solid var(--color-danger);
		border-radius: var(--radius-md);
		font-size: var(--text-sm);
	}

	.btn-primary {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 0.4rem;
		padding: 0.6rem 1rem;
		background: var(--color-primary);
		color: #fff;
		border: none;
		border-radius: var(--radius-md);
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
</style>
