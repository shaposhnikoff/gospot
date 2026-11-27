import { mount } from 'svelte';
const settings = /^\/manage(?:\/|$)/.test(location.pathname);
if (settings) {
  mount((await import('./AdminWorkspace.svelte')).default, {target: document.getElementById('app')!});
} else if (location.hash === '#console') {
  // Catalog of every classic function; the classic dashboard is the default view.
  mount((await import('./App.svelte')).default, {target: document.getElementById('app')!});
} else {
  mount((await import('./ClassicDashboard.svelte')).default, {target: document.getElementById('app')!});
}
