<script lang="ts">
	import '../app.css';

	// Svelte 4 slot syntax; child route content is rendered via <slot />.
</script>

<nav class="topnav">
	<a href="/">Overview</a>
	<a href="/channels">Channels</a>
	<a href="/categories">Categories</a>
	<a href="/history">History</a>
	<a href="/settings">Settings</a>
</nav>

<main class="content">
	<slot />
</main>

<style>
	:global(body) {
		margin: 0;
		font-family:
			system-ui,
			-apple-system,
			'Segoe UI',
			Roboto,
			sans-serif;
		background: #f6f7f9;
		color: #1a1a1a;
	}
	.topnav {
		display: flex;
		gap: 1rem;
		padding: 0.75rem 1.25rem;
		background: #1f2933;
		color: #fff;
	}
	.topnav a {
		color: #cbd2d9;
		text-decoration: none;
		font-size: 0.95rem;
	}
	.topnav a:hover {
		color: #fff;
	}
	.content {
		padding: 1.25rem;
		max-width: 960px;
		margin: 0 auto;
	}
</style>
