import { mount } from 'svelte';
import { unmount } from 'svelte';
const settings = /^\/manage(?:\/|$)/.test(location.pathname);
if (settings) {
  // /manage/ with no section is the classic /admin/ page; other sections are
  // still the native workspace pages until they are ported.
  const target = document.getElementById('app')!;
  const pages = {
    admin: () => import('./ClassicDashboard.svelte'), logs: () => import('./LiveLogs.svelte'),
    power: () => import('./Power.svelte'), update: () => import('./Update.svelte'),
    workspace: () => import('./AdminWorkspace.svelte'),
  };
  const page = (): keyof typeof pages => {
    const h = location.hash.replace(/^#/, '');
    return h === '' ? 'admin' : h in pages && h !== 'workspace' ? h as keyof typeof pages : 'workspace';
  };
  let shown: string | null = null;
  let app: ReturnType<typeof mount> | null = null;
  const route = async () => {
    const next = page();
    if (shown === next) return;
    shown = next;
    const component = (await pages[next]()).default;
    if (shown !== next) return;
    if (app) void unmount(app);
    target.replaceChildren();
    app = mount(component as never, { target, props: next === 'admin' ? { admin: true } : {} });
  };
  window.addEventListener('hashchange', () => void route());
  void route();
} else if (location.hash === '#console') {
  // Catalog of every classic function; the classic dashboard is the default view.
  mount((await import('./App.svelte')).default, {target: document.getElementById('app')!});
} else {
  mount((await import('./ClassicDashboard.svelte')).default, {target: document.getElementById('app')!});
}
