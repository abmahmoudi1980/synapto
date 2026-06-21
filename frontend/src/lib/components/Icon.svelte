<script lang="ts">
	// SVG icon component (Heroicons-style outline). No emojis.
	// Usage: <Icon name="check" size={16} />

	export let name: IconName;
	export let size: number = 18;
	export let strokeWidth: number = 1.75;
	export let label: string | null = null;

	type IconName =
		| 'check'
		| 'check-circle'
		| 'minus'
		| 'x'
		| 'x-circle'
		| 'plus'
		| 'trash'
		| 'pencil'
		| 'cog'
		| 'home'
		| 'channel'
		| 'tag'
		| 'history'
		| 'arrow-right'
		| 'arrow-left'
		| 'search'
		| 'info'
		| 'warning'
		| 'alert'
		| 'spinner'
		| 'menu'
		| 'sun'
		| 'moon'
		| 'logo';

	$: titleId = label ? `icon-title-${Math.random().toString(36).slice(2, 8)}` : undefined;
</script>

<svg
	xmlns="http://www.w3.org/2000/svg"
	viewBox="0 0 24 24"
	width={size}
	height={size}
	fill="none"
	stroke="currentColor"
	stroke-width={strokeWidth}
	stroke-linecap="round"
	stroke-linejoin="round"
	role={label ? 'img' : 'presentation'}
	aria-label={label ?? undefined}
	aria-hidden={label ? undefined : 'true'}
	aria-labelledby={titleId}
	focusable="false"
>
	{#if label && titleId}
		<title id={titleId}>{label}</title>
	{/if}
	{#if name === 'check'}
		<polyline points="20 6 9 17 4 12" />
	{:else if name === 'check-circle'}
		<circle cx="12" cy="12" r="9" />
		<polyline points="9 12 11 14 15 10" />
	{:else if name === 'x'}
		<line x1="18" y1="6" x2="6" y2="18" />
		<line x1="6" y1="6" x2="18" y2="18" />
	{:else if name === 'minus'}
		<line x1="5" y1="12" x2="19" y2="12" />
	{:else if name === 'x-circle'}
		<circle cx="12" cy="12" r="9" />
		<line x1="9" y1="9" x2="15" y2="15" />
		<line x1="15" y1="9" x2="9" y2="15" />
	{:else if name === 'plus'}
		<line x1="12" y1="5" x2="12" y2="19" />
		<line x1="5" y1="12" x2="19" y2="12" />
	{:else if name === 'trash'}
		<polyline points="3 6 5 6 21 6" />
		<path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
		<path d="M10 11v6M14 11v6" />
		<path d="M9 6V4a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2" />
	{:else if name === 'pencil'}
		<path d="M12 20h9" />
		<path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5z" />
	{:else if name === 'cog'}
		<circle cx="12" cy="12" r="3" />
		<path
			d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06A1.65 1.65 0 0 0 9 4.6 1.65 1.65 0 0 0 10 3.09V3a2 2 0 0 1 4 0v.09A1.65 1.65 0 0 0 15 4.6a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9c.13.27.21.55.21.86V10a1.65 1.65 0 0 0 1 1.51H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
		/>
	{:else if name === 'home'}
		<path d="M3 12l9-9 9 9" />
		<path d="M5 10v10a1 1 0 0 0 1 1h3v-6h6v6h3a1 1 0 0 0 1-1V10" />
	{:else if name === 'channel'}
		<path d="M3 11l18-8-8 18-2-8-8-2z" />
	{:else if name === 'tag'}
		<path d="M20.59 13.41L13.42 20.58a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z" />
		<line x1="7" y1="7" x2="7.01" y2="7" />
	{:else if name === 'history'}
		<path d="M1 4v6h6" />
		<path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10" />
		<polyline points="12 7 12 12 15 15" />
	{:else if name === 'arrow-right'}
		<line x1="5" y1="12" x2="19" y2="12" />
		<polyline points="12 5 19 12 12 19" />
	{:else if name === 'arrow-left'}
		<line x1="19" y1="12" x2="5" y2="12" />
		<polyline points="12 19 5 12 12 5" />
	{:else if name === 'search'}
		<circle cx="11" cy="11" r="7" />
		<line x1="21" y1="21" x2="16.65" y2="16.65" />
	{:else if name === 'info'}
		<circle cx="12" cy="12" r="9" />
		<line x1="12" y1="11" x2="12" y2="16" />
		<line x1="12" y1="8" x2="12.01" y2="8" />
	{:else if name === 'warning'}
		<path
			d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
		/>
		<line x1="12" y1="9" x2="12" y2="13" />
		<line x1="12" y1="17" x2="12.01" y2="17" />
	{:else if name === 'alert'}
		<circle cx="12" cy="12" r="9" />
		<line x1="12" y1="8" x2="12" y2="12" />
		<line x1="12" y1="16" x2="12.01" y2="16" />
	{:else if name === 'spinner'}
		<path d="M21 12a9 9 0 1 1-6.22-8.56" />
	{:else if name === 'menu'}
		<line x1="3" y1="6" x2="21" y2="6" />
		<line x1="3" y1="12" x2="21" y2="12" />
		<line x1="3" y1="18" x2="21" y2="18" />
	{:else if name === 'sun'}
		<circle cx="12" cy="12" r="4" />
		<line x1="12" y1="2" x2="12" y2="4" />
		<line x1="12" y1="20" x2="12" y2="22" />
		<line x1="4.93" y1="4.93" x2="6.34" y2="6.34" />
		<line x1="17.66" y1="17.66" x2="19.07" y2="19.07" />
		<line x1="2" y1="12" x2="4" y2="12" />
		<line x1="20" y1="12" x2="22" y2="12" />
		<line x1="4.93" y1="19.07" x2="6.34" y2="17.66" />
		<line x1="17.66" y1="6.34" x2="19.07" y2="4.93" />
	{:else if name === 'moon'}
		<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
	{:else if name === 'logo'}
		<!-- Synapto mark: a stylized neural synapse. -->
		<circle cx="6" cy="12" r="2.5" />
		<circle cx="18" cy="6" r="2.5" />
		<circle cx="18" cy="18" r="2.5" />
		<line x1="8" y1="11" x2="16" y2="7" />
		<line x1="8" y1="13" x2="16" y2="17" />
	{/if}
</svg>

<style>
	svg {
		display: inline-block;
		vertical-align: -0.125em;
		flex-shrink: 0;
	}
</style>
