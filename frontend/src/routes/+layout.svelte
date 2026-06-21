<script context="module" lang="ts">
	function formatUptime(s: number): string {
		if (s < 60) return `${s}s`;
		const m = Math.floor(s / 60);
		if (m < 60) return `${m}m`;
		const h = Math.floor(m / 60);
		const rem = m % 60;
		if (h < 24) return rem > 0 ? `${h}h ${rem}m` : `${h}h`;
		const d = Math.floor(h / 24);
		const remH = h % 24;
		return remH > 0 ? `${d}d ${remH}h` : `${d}d`;
	}
</script>

<script lang="ts">
	import '../app.css';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import Icon from '$lib/components/Icon.svelte';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import { onMount } from 'svelte';
	import { api, type Health } from '$lib/api';

	type NavItem = {
		href: string;
		label: string;
		icon: 'home' | 'channel' | 'tag' | 'history' | 'cog';
	};

	const nav: NavItem[] = [
		{ href: '/', label: 'Overview', icon: 'home' },
		{ href: '/channels', label: 'Channels', icon: 'channel' },
		{ href: '/categories', label: 'Categories', icon: 'tag' },
		{ href: '/history', label: 'History', icon: 'history' },
		{ href: '/settings', label: 'Settings', icon: 'cog' }
	];

	let health: Health | null = null;
	let healthError = '';
	let authRequired = false;

	$: currentPath = $page.url.pathname;
	$: isLoginRoute = currentPath === '/login';
	$: isActive = (href: string) =>
		href === '/'
			? currentPath === '/'
			: currentPath === href || currentPath.startsWith(href + '/');

	onMount(async () => {
		// Auth gate. If auth is required and we're not on the login
		// page, probe /api/auth/status; if unauthenticated, redirect
		// to /login (which itself probes status and bounces back
		// here on success).
		try {
			const status = await api.getAuthStatus();
			authRequired = status.auth_required;
			if (status.auth_required && !status.authenticated && !isLoginRoute) {
				await goto('/login');
				return;
			}
			if (isLoginRoute && (!status.auth_required || status.authenticated)) {
				await goto('/');
				return;
			}
		} catch {
			// Status check failed; if we're not on /login, route there
			// so the user sees a clear error.
			if (!isLoginRoute) {
				await goto('/login');
				return;
			}
		}

		// Health snapshot for the sidebar service card.
		try {
			health = await api.getHealth();
		} catch (e) {
			healthError = (e as Error).message;
		}
	});

	async function handleLogout() {
		try {
			await api.logout();
		} catch {
			// ignore — we redirect regardless
		}
		await goto('/login');
	}

	function statusKind(h: Health | null): 'ok' | 'unreachable' | 'warning' {
		if (!h) return 'unreachable';
		if (!h.db_ok) return 'warning';
		if (h.status === 'ok' || h.status === 'degraded') return 'ok';
		return 'warning';
	}
	function statusLabel(h: Health | null): string {
		if (!h) return 'unreachable';
		if (!h.db_ok) return 'db error';
		return h.status;
	}
</script>

<a href="#main" class="skip-link">Skip to content</a>

{#if isLoginRoute}
	<slot />
{:else}
	<div class="layout">
		<aside class="sidebar" aria-label="Primary navigation">
			<div class="brand">
				<span class="brand-mark" aria-hidden="true">
					<Icon name="logo" size={22} strokeWidth={1.75} />
				</span>
				<div class="brand-text">
					<span class="brand-name">Synapto</span>
					<span class="brand-sub">Admin Panel</span>
				</div>
				{#if authRequired}
					<button
						type="button"
						class="logout-btn"
						on:click={handleLogout}
						title="Sign out"
						aria-label="Sign out"
					>
						<Icon name="arrow-left" size={14} />
					</button>
				{/if}
			</div>

			<nav class="nav" aria-label="Sections">
				{#each nav as item (item.href)}
					<a
						href={item.href}
						class="nav-item"
						class:active={isActive(item.href)}
						aria-current={isActive(item.href) ? 'page' : undefined}
					>
						<span class="nav-icon" aria-hidden="true">
							<Icon name={item.icon} size={18} />
						</span>
						<span class="nav-label">{item.label}</span>
					</a>
				{/each}
			</nav>

			<div class="health-card surface">
				<div class="health-head">
					<span class="health-title">Service</span>
					<StatusBadge kind={statusKind(health)} label={statusLabel(health)} />
				</div>
				{#if health}
					<dl class="health-grid">
						<div>
							<dt>Version</dt>
							<dd>{health.version}</dd>
						</div>
						<div>
							<dt>Uptime</dt>
							<dd>{formatUptime(health.uptime_seconds)}</dd>
						</div>
						<div>
							<dt>Scheduler</dt>
							<dd>{health.scheduler_state}</dd>
						</div>
					</dl>
				{:else if healthError}
					<p class="health-error">{healthError}</p>
				{:else}
					<p class="health-loading">Connecting…</p>
				{/if}
			</div>
		</aside>

		<main id="main" class="content" tabindex="-1">
			<slot />
		</main>
	</div>
{/if}

<style>
	.layout {
		display: grid;
		grid-template-columns: 248px 1fr;
		min-height: 100vh;
	}

	/* Sidebar */
	.sidebar {
		background: var(--color-surface);
		border-right: 1px solid var(--color-border);
		padding: var(--space-5);
		display: flex;
		flex-direction: column;
		gap: var(--space-5);
		position: sticky;
		top: 0;
		height: 100vh;
	}

	.brand {
		display: flex;
		align-items: center;
		gap: 0.7rem;
		padding: 0 var(--space-2);
	}
	.brand-mark {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 36px;
		height: 36px;
		border-radius: var(--radius-md);
		background: var(--color-primary-soft);
		color: var(--color-primary);
	}
	.brand-text {
		display: flex;
		flex-direction: column;
		line-height: 1.1;
	}
	.brand-name {
		font-weight: 700;
		font-size: var(--text-lg);
		letter-spacing: -0.01em;
	}
	.brand-sub {
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
		letter-spacing: 0.04em;
		text-transform: uppercase;
	}

	.logout-btn {
		margin-left: auto;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 28px;
		height: 28px;
		background: transparent;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-sm);
		color: var(--color-text-muted);
		cursor: pointer;
		transition:
			background var(--duration-base) var(--ease-out),
			color var(--duration-base) var(--ease-out);
	}
	.logout-btn:hover {
		background: var(--color-surface-2);
		color: var(--color-text);
	}

	.nav {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.nav-item {
		display: flex;
		align-items: center;
		gap: 0.7rem;
		padding: 0.5rem 0.75rem;
		border-radius: var(--radius-md);
		color: var(--color-text-muted);
		font-size: var(--text-sm);
		font-weight: 500;
		transition:
			background var(--duration-base) var(--ease-out),
			color var(--duration-base) var(--ease-out);
	}
	.nav-item:hover {
		background: var(--color-surface-2);
		color: var(--color-text);
		text-decoration: none;
	}
	.nav-item.active {
		background: var(--color-primary-soft);
		color: var(--color-primary-soft-text);
	}
	.nav-item.active .nav-icon {
		color: var(--color-primary);
	}
	.nav-icon {
		display: inline-flex;
		color: currentColor;
		opacity: 0.8;
	}

	.health-card {
		margin-top: auto;
		padding: var(--space-3) var(--space-4);
		font-size: var(--text-sm);
	}
	.health-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
		margin-bottom: 0.5rem;
	}
	.health-title {
		font-weight: 600;
		color: var(--color-text);
		font-size: var(--text-xs);
		letter-spacing: 0.04em;
		text-transform: uppercase;
	}
	.health-grid {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.4rem 0.75rem;
		margin: 0;
	}
	.health-grid div {
		min-width: 0;
	}
	.health-grid dt {
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}
	.health-grid dd {
		margin: 0;
		font-size: var(--text-sm);
		font-weight: 500;
		color: var(--color-text);
		font-family: var(--font-mono);
	}
	.health-error,
	.health-loading {
		margin: 0;
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
		font-style: italic;
	}
	.health-error {
		color: var(--color-danger);
		font-style: normal;
	}

	/* Content */
	.content {
		padding: var(--space-8) var(--space-6);
		max-width: 1100px;
		width: 100%;
	}
	.content:focus {
		outline: none;
	}

	@media (max-width: 768px) {
		.layout {
			grid-template-columns: 1fr;
		}
		.sidebar {
			position: static;
			height: auto;
			border-right: none;
			border-bottom: 1px solid var(--color-border);
		}
		.nav {
			flex-direction: row;
			overflow-x: auto;
			padding-bottom: 0.25rem;
		}
		.nav-item {
			white-space: nowrap;
		}
		.health-card {
			margin-top: 0;
		}
	}
</style>
