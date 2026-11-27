<script lang="ts">
  // Shared page frame of the classic admin pages (live_modem_log.php,
  // power.php, update.php, ...): banner, menu, content, web-config footer.
  import { onMount, type Snippet } from 'svelte';
  import { operatorOrigin } from './runtime';
  import { applyTheme } from './classic';

  let { title, links, children, menu, footer = 'config' }: { title: string; links: string[]; children: Snippet; menu?: Snippet; footer?: 'config' | 'dashboard' } = $props();
  const targets: Record<string, string> = {
    Dashboard: '', Admin: '/manage/', 'Live Logs': '/manage/#logs', Power: '/manage/#power', Update: '/manage/#update',
    'Backup/Restore': '/manage/#backup', Configuration: '/manage/#configuration', Expert: '/manage/#expert',
    Calibrate: '/manage/#calibration',
  };
  let operator = $state('/');
  let versions = $state('');
  const year = new Date().getFullYear();

  onMount(() => {
    document.body.classList.add('pistar-classic');
    void operatorOrigin().then(o => { if (o) operator = o + '/'; });
    void fetch('/manage/api/dashboard', { cache: 'no-store' }).then(r => r.ok ? r.json() : null).then(d => {
      if (d) versions = `Pi-Star:${d.PiStar || 'native'} / Dashboard: ${d.Version}`;
      applyTheme(d?.Theme);
    }).catch(() => {});
    return () => document.body.classList.remove('pistar-classic');
  });
</script>

<svelte:head><title>Pi-Star - Digital Voice Dashboard - {title}</title></svelte:head>

<div class="container pistar-frame" id="top">
  <div class="header">
    <div style="font-size: 8px; text-align: right; padding-right: 8px;">{versions}</div>
    <h1>Pi-Star Digital Voice - {title}</h1>
    <p style="padding-right: 5px; text-align: right; color: #ffffff;">
      {#each links as label, i}<a href={label === 'Dashboard' ? operator : targets[label]} style="color: #ffffff;">{label}</a>{#if i < links.length - 1}{' | '}{/if}{/each}
    </p>
    {#if menu}{@render menu()}{/if}
  </div>
  <div class="contentwide">
    {@render children()}
  </div>
  <div class="footer">
    {#if footer === 'dashboard'}Pi-Star / Pi-Star Dashboard{:else}Pi-Star web config{/if}, &copy; Andy Taylor (MW0MWZ) 2014-{year}.<br />
    Need help? Click <a style="color: #ffffff;" href="https://www.facebook.com/groups/pistarusergroup/" target="_blank" rel="noopener noreferrer">here for the Support Group</a><br />
    Get your copy of Pi-Star from <a style="color: #ffffff;" href="http://www.pistar.uk/downloads/" target="_blank" rel="noopener noreferrer">here</a>.<br />
    <br />
  </div>
</div>

<style>
  /* css/pistar-css.php (built-in palette), applied to the page's own content too. */
  :global(body.pistar-classic) { margin: 8px; font: 12px verdana, arial, sans-serif; background: var(--ps-page, #edf0f5); color: #000000; }
  .container { width: 820px; text-align: left; margin: auto; border-radius: 10px; box-shadow: 3px 3px 3px #707070; background: var(--ps-content, #ffffff); }
  .header { background: var(--ps-banners, #dd4b39); color: var(--ps-banner-text, #ffffff); font-family: verdana, arial, sans-serif; text-align: left; padding: 5px 0; border-radius: 10px 10px 0 0; }
  .header p { margin: 12px 0; }
  .contentwide { padding: 5px; color: var(--ps-text, #000000); background: var(--ps-content, #ffffff); text-align: center; }
  .footer { background: var(--ps-banners, #dd4b39); color: var(--ps-banner-text, #ffffff); font-family: verdana, arial, sans-serif; font-size: 9px; text-align: center; padding: 10px 0; border-radius: 0 0 10px 10px; clear: both; }
  h1 { text-shadow: 2px 2px var(--ps-banner-drop, #303030); text-align: center; font-size: 2em; margin: 0.67em 0; }
  .pistar-frame :global(table) { vertical-align: middle; text-align: center; empty-cells: show; padding: 0; border-collapse: collapse; border: 2px solid #000000; color: #ffffff; background: #000000; font-family: verdana, arial, sans-serif; width: 100%; white-space: nowrap; }
  .pistar-frame :global(th) { font-family: "Lucidia Console", Monaco, monospace; text-shadow: 1px 1px var(--ps-head-drop, #8b0000); background: var(--ps-banners, #dd4b39); border: 1px solid #c0c0c0; }
  .pistar-frame :global(tr:nth-child(even)) { background: var(--ps-even, #f7f7f7); }
  .pistar-frame :global(tr:nth-child(odd)) { background: var(--ps-odd, #d0d0d0); }
  .pistar-frame :global(td) { color: #000000; font-family: "Lucidia Console", Monaco, monospace; border: 1px solid #000000; }
  .pistar-frame :global(a:link), .pistar-frame :global(a:visited) { text-decoration: none; color: #0000e0; }
  .pistar-frame :global(#tail) { height: 450px; width: 805px; max-width: 100%; overflow-y: scroll; overflow-x: scroll; color: #00ff00; background: #000000; white-space: pre-line; text-align: left; }
  .pistar-frame :global(button.icon) { border: none; background: none; cursor: pointer; }
  @media screen and (max-width: 829px) {
    .container { width: 100%; }
    .contentwide { font-size: 1.4em; }
  }
</style>
