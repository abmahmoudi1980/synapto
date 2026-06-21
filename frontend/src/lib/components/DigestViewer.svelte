<script lang="ts">
	import type { DigestInfo, ItemsByCategory } from '$lib/api';
	import StatusBadge from './StatusBadge.svelte';
	import Icon from './Icon.svelte';

	export let digest: DigestInfo;
	export let itemsByCategory: ItemsByCategory[];

	$: statusKind = computeStatusKind(digest.send_status, digest.degraded);
	$: statusLabel = computeStatusLabel(digest.send_status, digest.degraded);

	function computeStatusKind(
		s: 'ok' | 'failed' | 'blocked',
		degraded: boolean
	): 'ok' | 'degraded' | 'failed' | 'warning' {
		if (s === 'failed') return 'failed';
		if (s === 'blocked') return 'warning';
		if (degraded) return 'degraded';
		return 'ok';
	}
	function computeStatusLabel(s: 'ok' | 'failed' | 'blocked', degraded: boolean): string {
		if (s === 'failed') return 'send failed';
		if (s === 'blocked') return 'subscriber blocked';
		if (degraded) return 'degraded';
		return 'ok';
	}

	function mediaKindLabel(k: string): string {
		if (k === 'text') return '';
		if (k === 'image') return 'Image';
		if (k === 'video') return 'Video';
		if (k === 'voice') return 'Voice';
		return 'Media';
	}

	function formatDate(iso: string): string {
		try {
			return new Date(iso).toLocaleString();
		} catch {
			return iso;
		}
	}

	function percent(n: number | null): string {
		if (n == null) return '—';
		return `${Math.round(n * 100)}%`;
	}

	// Total items across all categories (used in the header).
	$: totalItems = itemsByCategory.reduce((sum, g) => sum + g.items.length, 0);

	// Sanitize the rendered_text for HTML display: Telegram MarkdownV2
	// uses \_, \*, \` for escaping. We display it as a monospaced
	// preformatted block, not as rendered HTML.
</script>

<article class="digest-viewer">
	<header class="digest-head surface">
		<div class="head-row">
			<div class="head-left">
				<h2>Digest</h2>
				<p class="meta">Sent {formatDate(digest.sent_at)}</p>
			</div>
			<StatusBadge kind={statusKind} label={statusLabel} size="md" />
		</div>
		<dl class="head-meta">
			<div>
				<dt>Items</dt>
				<dd class="num">{totalItems}</dd>
			</div>
			<div>
				<dt>Telegram msg id</dt>
				<dd class="mono">{digest.telegram_msg_id ?? '—'}</dd>
			</div>
			<div>
				<dt>Degraded</dt>
				<dd>{digest.degraded ? 'yes' : 'no'}</dd>
			</div>
		</dl>
	</header>

	{#if itemsByCategory.length > 0}
		<section class="surface section" aria-labelledby="items-heading">
			<h3 id="items-heading">Items by category</h3>
			{#each itemsByCategory as group (group.category.id)}
				<div class="group">
					<header class="group-head">
						<span class="group-name">
							<span class="hash" aria-hidden="true">#</span>
							{group.category.name}
						</span>
						<span class="group-meta">
							{group.items.length}
							{group.items.length === 1 ? 'item' : 'items'}
							{#if group.category.is_default}
								<span class="default-pill">default</span>
							{/if}
						</span>
					</header>
					<ul class="item-list">
						{#each group.items as item (item.id)}
							<li class="item">
								<span class="item-summary">
									{#if mediaKindLabel(item.media_kind)}
										<span class="media-tag"
											>[{mediaKindLabel(item.media_kind)}]</span
										>
									{/if}
									{item.summary}
								</span>
								<span class="item-channel">
									<Icon name="channel" size={12} />
									@{item.channel.handle}
								</span>
								{#if item.confidence != null}
									<span class="item-confidence" title="AI confidence">
										{percent(item.confidence)}
									</span>
								{/if}
							</li>
						{/each}
					</ul>
				</div>
			{/each}
		</section>
	{/if}

	<section class="surface section" aria-labelledby="raw-heading">
		<h3 id="raw-heading">What Telegram received</h3>
		<p class="hint">
			The exact MarkdownV2 text that was sent to the subscriber. Backslashes escape reserved
			characters per Telegram's MarkdownV2 grammar.
		</p>
		<pre class="raw-text"><code>{digest.rendered_text}</code></pre>
	</section>
</article>

<style>
	.digest-viewer {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	.digest-head {
		padding: var(--space-4) var(--space-5);
	}
	.head-row {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: var(--space-3);
		margin-bottom: var(--space-3);
	}
	.head-left h2 {
		font-size: var(--text-lg);
		margin: 0;
	}
	.meta {
		margin: 0.2rem 0 0;
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
		font-family: var(--font-mono);
	}
	.head-meta {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
		gap: var(--space-3) var(--space-4);
		margin: 0;
	}
	.head-meta dt {
		font-size: var(--text-xs);
		font-weight: 600;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--color-text-subtle);
		margin-bottom: 0.2rem;
	}
	.head-meta dd {
		margin: 0;
		font-size: var(--text-sm);
		color: var(--color-text);
	}
	.head-meta .num {
		font-weight: 600;
	}
	.head-meta .mono {
		font-family: var(--font-mono);
	}

	.section {
		padding: var(--space-4) var(--space-5);
	}
	.section h3 {
		font-size: var(--text-lg);
		margin: 0 0 var(--space-3);
	}
	.hint {
		margin: 0 0 var(--space-2);
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
	}

	.group {
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		padding: var(--space-3) var(--space-4);
		margin-bottom: var(--space-3);
		background: var(--color-surface);
	}
	.group:last-child {
		margin-bottom: 0;
	}
	.group-head {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 0.5rem;
		padding-bottom: var(--space-2);
		margin-bottom: var(--space-2);
		border-bottom: 1px solid var(--color-border);
	}
	.group-name {
		font-size: var(--text-base);
		font-weight: 600;
		color: var(--color-text);
		display: inline-flex;
		align-items: baseline;
		gap: 0.25rem;
	}
	.group-name .hash {
		color: var(--color-text-subtle);
		font-weight: 400;
	}
	.group-meta {
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
	}
	.default-pill {
		font-size: 0.65rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--color-text-muted);
		background: var(--color-surface-2);
		padding: 0.1rem 0.4rem;
		border-radius: var(--radius-pill);
	}

	.item-list {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.item {
		display: grid;
		grid-template-columns: 1fr auto auto;
		gap: 0.5rem 0.75rem;
		align-items: center;
		padding: 0.4rem 0;
		border-bottom: 1px solid var(--color-border);
	}
	.item:last-child {
		border-bottom: none;
	}
	.item-summary {
		font-size: var(--text-sm);
		color: var(--color-text);
		line-height: 1.4;
	}
	.media-tag {
		display: inline-block;
		margin-right: 0.3rem;
		padding: 0.05rem 0.35rem;
		font-size: var(--text-xs);
		font-weight: 600;
		color: var(--color-primary-soft-text);
		background: var(--color-primary-soft);
		border-radius: var(--radius-sm);
	}
	.item-channel {
		display: inline-flex;
		align-items: center;
		gap: 0.2rem;
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--color-text-muted);
		white-space: nowrap;
	}
	.item-confidence {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--color-text-subtle);
		white-space: nowrap;
	}

	.raw-text {
		margin: 0;
		padding: var(--space-3);
		background: var(--color-surface-2);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		line-height: 1.5;
		white-space: pre-wrap;
		word-wrap: break-word;
		overflow-x: auto;
		color: var(--color-text);
		max-height: 480px;
		overflow-y: auto;
	}
</style>
