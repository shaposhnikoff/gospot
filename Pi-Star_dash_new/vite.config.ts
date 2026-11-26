import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({ base: './', build: {target:'es2022'}, plugins: [svelte()] });
