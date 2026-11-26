import { mount } from 'svelte';
const settings = /^\/manage(?:\/|$)/.test(location.pathname);
if (settings) {
  mount((await import('./AdminWorkspace.svelte')).default, {target: document.getElementById('app')!});
} else {
  mount((await import('./App.svelte')).default, {target: document.getElementById('app')!});
}
