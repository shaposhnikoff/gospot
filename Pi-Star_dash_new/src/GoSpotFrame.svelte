<script lang="ts">
  // Shared page frame of the classic admin pages (live_modem_log.php,
  // power.php, update.php, ...): banner, menu, content, web-config footer.
  import { onMount, type Snippet } from 'svelte';
  import { operatorOrigin } from './runtime';
  import { applyTheme } from './classic';

  let { title, links, children, menu, footer = 'config', actions = {} }: {
    title: string; links: string[]; children: Snippet; menu?: Snippet; footer?: 'config' | 'dashboard';
    actions?: Record<string, () => void>; // links that run a page action (configure.php "Factory Reset")
  } = $props();
  const targets: Record<string, string> = {
    Dashboard: '', Admin: '/manage/', 'Live Logs': '/manage/#logs', Power: '/manage/#power', Update: '/manage/#update',
    'Backup/Restore': '/manage/#backup', Configuration: '/manage/#configuration', Expert: '/manage/#expert',
    Calibrate: '/manage/#calibration',
  };
  let operator = $state('/');
  let versions = $state('');

  onMount(() => {
    document.body.classList.add('gospot-classic');
    void operatorOrigin().then(o => { if (o) operator = o + '/'; });
    void fetch('/manage/api/dashboard', { cache: 'no-store' }).then(r => r.ok ? r.json() : null).then(d => {
      if (d) versions = `GoSpot: ${d.Version}`;
      applyTheme(d?.Theme);
    }).catch(() => {});
    return () => document.body.classList.remove('gospot-classic');
  });
</script>

<svelte:head><title>GoSpot - Digital Voice Dashboard - {title}</title></svelte:head>

<div class="container gospot-frame" id="top">
  <div class="header">
    <div style="font-size: 8px; text-align: right; padding-right: 8px;">{versions}</div>
    <h1>GoSpot Digital Voice - {title}</h1>
    <p style="padding-right: 5px; text-align: right; color: #ffffff;">
      {#each links as label, i}{#if actions[label]}<a href="#top" style="color: #ffffff;" onclick={e => { e.preventDefault(); actions[label](); }}>{label}</a>{:else}<a href={label === 'Dashboard' ? operator : targets[label]} style="color: #ffffff;">{label}</a>{/if}{' | '}{/each}<a href={operator + 'kiosk'} style="color: #ffffff;">Kiosk</a>
    </p>
    {#if menu}{@render menu()}{/if}
  </div>
  <div class="contentwide">
    {@render children()}
  </div>
  <div class="footer">
    {#if footer === 'dashboard'}GoSpot Dashboard{:else}GoSpot web config{/if} — Pi-Star compatible.<br />
    Need help? Click <a style="color: #ffffff;" href="https://github.com/shaposhnikoff/gospot/issues" target="_blank" rel="noopener noreferrer">here for the GoSpot issues</a><br />
    Get your copy of GoSpot from <a style="color: #ffffff;" href="https://github.com/shaposhnikoff/gospot/releases" target="_blank" rel="noopener noreferrer">here</a>.<br />
    <br />
  </div>
</div>

<style>
  /* css/pistar-css.php (built-in palette), applied to the page's own content too. */
  :global(body.gospot-classic) { margin: 8px; font: 12px verdana, arial, sans-serif; background: var(--ps-page, #edf0f5); color: #000000; }
  .container { width: 820px; text-align: left; margin: auto; border-radius: 10px; box-shadow: 3px 3px 3px #707070; background: var(--ps-content, #ffffff); }
  .header { background: var(--ps-banners, #dd4b39); color: var(--ps-banner-text, #ffffff); font-family: verdana, arial, sans-serif; text-align: left; padding: 5px 0; border-radius: 10px 10px 0 0; }
  .header p { margin: 12px 0; }
  .contentwide { padding: 5px; color: var(--ps-text, #000000); background: var(--ps-content, #ffffff); text-align: center; }
  .footer { background: var(--ps-banners, #dd4b39); color: var(--ps-banner-text, #ffffff); font-family: verdana, arial, sans-serif; font-size: 9px; text-align: center; padding: 10px 0; border-radius: 0 0 10px 10px; clear: both; }
  h1 { text-shadow: 2px 2px var(--ps-banner-drop, #303030); text-align: center; font-size: 2em; margin: 0.67em 0; }
  .gospot-frame :global(table) { vertical-align: middle; text-align: center; empty-cells: show; padding: 0; border-collapse: collapse; border: 2px solid #000000; color: #ffffff; background: #000000; font-family: verdana, arial, sans-serif; width: 100%; white-space: nowrap; }
  .gospot-frame :global(th) { font-family: "Lucidia Console", Monaco, monospace; text-shadow: 1px 1px var(--ps-head-drop, #8b0000); background: var(--ps-banners, #dd4b39); border: 1px solid #c0c0c0; }
  .gospot-frame :global(tr:nth-child(even)) { background: var(--ps-even, #f7f7f7); }
  .gospot-frame :global(tr:nth-child(odd)) { background: var(--ps-odd, #d0d0d0); }
  .gospot-frame :global(td) { color: #000000; font-family: "Lucidia Console", Monaco, monospace; border: 1px solid #000000; }
  .gospot-frame :global(a:link), .gospot-frame :global(a:visited) { text-decoration: none; color: #0000e0; }
  .gospot-frame :global(#tail) { height: 450px; width: 805px; max-width: 100%; overflow-y: scroll; overflow-x: scroll; color: #00ff00; background: #000000; white-space: pre-line; text-align: left; }
  .gospot-frame :global(button.icon) { border: none; background: none; cursor: pointer; }
  @media screen and (max-width: 829px) {
    .container { width: 100%; }
    .contentwide { font-size: 1.4em; }
  }
</style>
