<script lang="ts">
  import { onMount } from 'svelte';
  import StationSettings from './StationSettings.svelte';
  import Services from './Services.svelte';
  import SystemInfo from './SystemInfo.svelte';
  import LogView from './LogView.svelte';
  import Backup from './Backup.svelte';
  const pages = [
    ['settings','Configuration'], ['expert','Expert'], ['services','Сервисы · DMR'], ['system','Система'], ['logs','Live Logs'], ['backup','Backup / Restore']
  ] as const;
  const current = () => ['#expert-dmrgateway','#expert-mmdvmhost'].includes(location.hash) ? location.hash.slice(1) : pages.find(([id]) => '#'+id === location.hash)?.[0] || 'settings';
  let page = $state(current());
  onMount(() => {
    const change = () => { page=current(); };
    window.addEventListener('hashchange',change); return () => window.removeEventListener('hashchange',change);
  });
</script>
<svelte:head><title>Pi-Star · Администрирование</title></svelte:head>
<div class="workspace">
  <header><strong>Pi-Star · Admin</strong><a href={'http://' + location.hostname + ':8789/'}>Обзор узла ↗</a></header>
  <nav aria-label="Администрирование">
    {#each pages as p}<a href={'#'+p[0]} aria-current={p[0]===page?'page':undefined}>{p[1]}</a>{/each}
  </nav>
  {#if page === 'expert'}
    <section><h1>Expert</h1><h2>Quick Edit</h2><ul>
      <li><a href="#expert-mmdvmhost">MMDVMHost · DMR</a></li>
      <li><a href="#expert-dmrgateway">DMRGateway · сеть 1</a></li>
    </ul></section>
  {:else if page === 'expert-dmrgateway'}<Services editor="gateway" />
  {:else if page === 'expert-mmdvmhost'}<Services editor="host" />
  {:else if page === 'services'}<Services />
  {:else if page === 'system'}<SystemInfo />
  {:else if page === 'logs'}<LogView />
  {:else if page === 'backup'}<Backup />
  {:else}<StationSettings /><Services editor="host" />{/if}
</div>
<style>
  :global(body){margin:0;color:#291e2b;background:#f7f4f6;font:15px/1.5 system-ui,sans-serif}
  .workspace{max-width:1100px;margin:auto;padding:clamp(12px,3vw,32px)}
  header,nav{display:flex;align-items:center;flex-wrap:wrap;gap:8px}header{justify-content:space-between;margin-bottom:12px}
  nav{border-bottom:1px solid #d6cdd2;padding-bottom:10px;margin-bottom:20px}
  a{color:#722946;text-decoration:none;font-weight:650}nav a{border-radius:8px;padding:6px 10px}nav a[aria-current=page]{color:white;background:#662440}
</style>
