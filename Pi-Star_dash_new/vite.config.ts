import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// Images stay files: the HTTPS admin CSP allows img-src 'self' but not data: URIs.
export default defineConfig({ base: './', build: {target:'es2022', assetsInlineLimit: 0}, plugins: [svelte()] });
