import { mount } from 'svelte';
import { unmount } from 'svelte';
const settings = /^\/manage(?:\/|$)/.test(location.pathname);
if (settings) {
  // /manage/ with no section is the classic /admin/ page; other sections are
  // still the native workspace pages until they are ported.
  const target = document.getElementById('app')!;
  const classic = () => location.hash === '' || location.hash === '#' || location.hash === '#admin';
  const [Classic, Workspace] = await Promise.all([import('./ClassicDashboard.svelte'), import('./AdminWorkspace.svelte')]);
  let shown: boolean | null = null;
  let app: ReturnType<typeof mount> | null = null;
  const route = () => {
    if (shown === classic()) return;
    if (app) void unmount(app);
    shown = classic();
    target.replaceChildren();
    app = shown ? mount(Classic.default, { target, props: { admin: true } }) : mount(Workspace.default, { target });
  };
  window.addEventListener('hashchange', route);
  route();
} else if (location.hash === '#console') {
  // Catalog of every classic function; the classic dashboard is the default view.
  mount((await import('./App.svelte')).default, {target: document.getElementById('app')!});
} else {
  mount((await import('./ClassicDashboard.svelte')).default, {target: document.getElementById('app')!});
}
