module.exports = {
	root: true,
	env: {
		browser: true,
		es2022: true,
		node: true
	},
	extends: [
		'eslint:recommended',
		'plugin:@typescript-eslint/recommended',
		'plugin:svelte/recommended',
		'prettier'
	],
	overrides: [
		{
			files: ['*.svelte'],
			parser: 'svelte-eslint-parser',
			parserOptions: {
				parser: '@typescript-eslint/parser',
				extraFileExtensions: ['.svelte']
			},
			processor: 'svelte/svelte'
		}
	],
	parser: '@typescript-eslint/parser',
	parserOptions: {
		ecmaVersion: 2022,
		sourceType: 'module',
		extraFileExtensions: ['.svelte']
	},
	plugins: ['@typescript-eslint', 'svelte'],
	ignorePatterns: ['build/', '.svelte-kit/', 'node_modules/'],
	rules: {
		'no-unused-vars': 'off',
		'@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
		'@typescript-eslint/no-explicit-any': 'warn'
	}
};
