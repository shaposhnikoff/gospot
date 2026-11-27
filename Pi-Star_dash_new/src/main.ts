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
    expert: () => import('./Expert.svelte'),
    workspace: () => import('./AdminWorkspace.svelte'),
  };
  const page = (): keyof typeof pages => {
    const h = location.hash.replace(/^#/, '');
    if (h === 'expert' || h.startsWith('expert-')) return 'expert';
    return h === '' ? 'admin' : h in pages && h !== 'workspace' ? h as keyof typeof pages : 'workspace';
  };
  let shown: string | null = null;
  let app: ReturnType<typeof mount> | null = null;
  const route = async () => {
    const next = page();
    const props = next === 'admin' ? { admin: true } : next === 'expert' ? { page: location.hash.replace(/^#expert-?/, '') } : {};
    if (shown === next && next !== 'expert') return;
    if (shown === next && app) { void unmount(app); app = null; }
    shown = next;
    const component = (await pages[next]()).default;
    if (shown !== next) return;
    if (app) void unmount(app);
    target.replaceChildren();
    app = mount(component as never, { target, props });
  };
  window.addEventListener('hashchange', () => void route());
  void route();
} else if (location.hash === '#console') {
  // Catalog of every classic function; the classic dashboard is the default view.
  mount((await import('./App.svelte')).default, {target: document.getElementById('app')!});
} else {
  mount((await import('./ClassicDashboard.svelte')).default, {target: document.getElementById('app')!});
}
