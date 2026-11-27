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
    configuration: () => import('./Configure.svelte'),
    backup: () => import('./BackupRestore.svelte'),
    calibration: () => import('./Calibration.svelte'),
    workspace: () => import('./AdminWorkspace.svelte'),
  };
  const page = (): keyof typeof pages => {
    const h = location.hash.replace(/^#/, '');
    if (h === 'expert' || h.startsWith('expert-')) return 'expert';
    if (h === 'calibration-debug') return 'calibration';
    return h === '' ? 'admin' : h in pages && h !== 'workspace' ? h as keyof typeof pages : 'workspace';
  };
  let shown: string | null = null;
  let app: ReturnType<typeof mount> | null = null;
  let generation = 0;
  const route = async () => {
    const next = page();
    if (shown === next && next !== 'expert' && next !== 'calibration') return;
    // Only the latest navigation may mount: earlier imports can resolve later.
    const mine = ++generation;
    shown = next;
    const component = (await pages[next]()).default;
    if (mine !== generation) return;
    const props = next === 'admin' ? { admin: true } : next === 'expert' ? { page: location.hash.replace(/^#expert-?/, '') }
      : next === 'calibration' ? { debug: location.hash === '#calibration-debug' } : {};
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
