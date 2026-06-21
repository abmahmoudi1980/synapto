<script context="module" lang="ts">
	export type StatusKind =
		| 'ok'
		| 'success'
		| 'warning'
		| 'danger'
		| 'info'
		| 'neutral'
		| 'primary'
		| 'active'
		| 'inaccessible'
		| 'banned'
		| 'default'
		| 'custom'
		| 'idle'
		| 'running'
		| 'failed'
		| 'degraded'
		| 'skipped'
		| 'unreachable';

	export type StatusIcon =
		| 'check'
		| 'check-circle'
		| 'x'
		| 'x-circle'
		| 'info'
		| 'warning'
		| 'spinner'
		| 'minus';

	export const STATUS_CONFIG: Record<StatusKind, { fg: string; bg: string; icon: StatusIcon }> = {
		ok: { fg: 'var(--color-success)', bg: 'var(--color-success-soft)', icon: 'check-circle' },
		success: { fg: 'var(--color-success)', bg: 'var(--color-success-soft)', icon: 'check' },
		warning: { fg: 'var(--color-warning)', bg: 'var(--color-warning-soft)', icon: 'warning' },
		danger: { fg: 'var(--color-danger)', bg: 'var(--color-danger-soft)', icon: 'x-circle' },
		info: { fg: 'var(--color-info)', bg: 'var(--color-info-soft)', icon: 'info' },
		neutral: { fg: 'var(--color-text-muted)', bg: 'var(--color-surface-2)', icon: 'minus' },
		primary: {
			fg: 'var(--color-primary-soft-text)',
			bg: 'var(--color-primary-soft)',
			icon: 'info'
		},
		active: {
			fg: 'var(--color-success)',
			bg: 'var(--color-success-soft)',
			icon: 'check-circle'
		},
		inaccessible: {
			fg: 'var(--color-warning)',
			bg: 'var(--color-warning-soft)',
			icon: 'warning'
		},
		banned: { fg: 'var(--color-danger)', bg: 'var(--color-danger-soft)', icon: 'x-circle' },
		default: { fg: 'var(--color-text-muted)', bg: 'var(--color-surface-2)', icon: 'info' },
		custom: {
			fg: 'var(--color-primary-soft-text)',
			bg: 'var(--color-primary-soft)',
			icon: 'info'
		},
		idle: { fg: 'var(--color-text-muted)', bg: 'var(--color-surface-2)', icon: 'minus' },
		running: { fg: 'var(--color-info)', bg: 'var(--color-info-soft)', icon: 'spinner' },
		failed: { fg: 'var(--color-danger)', bg: 'var(--color-danger-soft)', icon: 'x-circle' },
		degraded: { fg: 'var(--color-warning)', bg: 'var(--color-warning-soft)', icon: 'warning' },
		skipped: { fg: 'var(--color-text-subtle)', bg: 'var(--color-surface-2)', icon: 'minus' },
		unreachable: {
			fg: 'var(--color-danger)',
			bg: 'var(--color-danger-soft)',
			icon: 'x-circle'
		}
	};
</script>

<script lang="ts">
	import Icon from './Icon.svelte';

	export let kind: StatusKind = 'neutral';
	export let label: string = '';
	export let size: 'sm' | 'md' = 'sm';
	export let showIcon: boolean = true;

	$: c = STATUS_CONFIG[kind] ?? STATUS_CONFIG.neutral;
</script>

<span class="badge" class:md={size === 'md'} style="--badge-fg: {c.fg}; --badge-bg: {c.bg};">
	{#if showIcon && c.icon === 'spinner'}
		<span class="icon spinner" aria-hidden="true">
			<Icon name="spinner" size={size === 'md' ? 14 : 12} strokeWidth={2.5} />
		</span>
	{:else if showIcon}
		<span class="icon" aria-hidden="true">
			<Icon name={c.icon} size={size === 'md' ? 14 : 12} strokeWidth={2.25} />
		</span>
	{/if}
	<span class="label">{label || kind}</span>
</span>

<style>
	.badge {
		display: inline-flex;
		align-items: center;
		gap: 0.3rem;
		padding: 0.18rem 0.55rem;
		border-radius: var(--radius-pill);
		background: var(--badge-bg);
		color: var(--badge-fg);
		font-size: var(--text-xs);
		font-weight: 600;
		line-height: 1;
		letter-spacing: 0.01em;
		white-space: nowrap;
		text-transform: lowercase;
	}
	.badge.md {
		padding: 0.3rem 0.7rem;
		font-size: var(--text-sm);
	}
	.icon {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		color: var(--badge-fg);
	}
	.spinner {
		animation: spin 0.8s linear infinite;
	}
	@keyframes spin {
		from {
			transform: rotate(0deg);
		}
		to {
			transform: rotate(360deg);
		}
	}
</style>
