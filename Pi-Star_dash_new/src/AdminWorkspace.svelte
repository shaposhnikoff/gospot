<script lang="ts">
  import { onMount } from 'svelte';
  import { readJSON, serviceOrigin } from './runtime';
  import StationSettings from './StationSettings.svelte';
  import Services from './Services.svelte';
  import Protocols from './Protocols.svelte';
  import SystemInfo from './SystemInfo.svelte';
  import LogView from './LogView.svelte';
  const pages = [
    ['configuration','Configuration'], ['settings','Настройки станции'], ['expert','Expert'], ['services','Сервисы · DMR'], ['system','Система'], ['logs','Live Logs']
  ] as const;
  const current = () => ['#expert-dmrgateway','#expert-mmdvmhost'].includes(location.hash) ? location.hash.slice(1) : pages.find(([id]) => '#'+id === location.hash)?.[0] || 'settings';
  let page = $state(current());
  let operator = $state<string | null>(null);
  let preview = $state(false);
  onMount(() => {
    void readJSON('/runtime.json').then(config => {
      if (config && typeof config === 'object' && 'preview' in config && config.preview === true) {
        preview = true;
        operator = location.origin;
        return;
      }
      if (config && typeof config === 'object' && 'operatorPort' in config)
        operator = serviceOrigin(location.origin, 'http:', config.operatorPort);
    }).catch(() => {});
    const change = () => { page=current(); };
    window.addEventListener('hashchange',change); return () => window.removeEventListener('hashchange',change);
  });
</script>
<svelte:head><title>GoSpot · Администрирование</title></svelte:head>
<div class="workspace">
  <header><strong>GoSpot · Admin{preview ? ' · тестовый стенд' : ''}</strong>{#if operator}<a href={operator + '/'} data-testid="admin-back-operator">Обзор узла →</a>{/if}</header>
  <nav aria-label="Администрирование">
    {#each pages as p}<a href={'#'+p[0]} aria-current={p[0]===page?'page':undefined}>{p[1]}</a>{/each}
  </nav>
  {#if page === 'expert'}
    <section><h1>Expert</h1><h2>Quick Edit</h2><ul>
      <li><a href="#expert-mmdvmhost">MMDVMHost · протоколы</a></li>
      <li><a href="#expert-dmrgateway">DMRGateway · сеть 1</a></li>
    </ul></section>
  {:else if page === 'configuration'}
    <Protocols />
  {:else if page === 'expert-dmrgateway'}<Services editor="gateway" />
  {:else if page === 'expert-mmdvmhost'}<Protocols />
  {:else if page === 'services'}<Services />
  {:else if page === 'system'}<SystemInfo />
  {:else if page === 'logs'}<LogView />
  {:else}<StationSettings />{/if}
</div>
<style>
  :global(body){margin:0;color:#291e2b;background:#f7f4f6;font:15px/1.5 system-ui,sans-serif}
  .workspace{max-width:1100px;margin:auto;padding:clamp(12px,3vw,32px)}
  header,nav{display:flex;align-items:center;flex-wrap:wrap;gap:8px}header{justify-content:space-between;margin-bottom:12px}
  nav{border-bottom:1px solid #d6cdd2;padding-bottom:10px;margin-bottom:20px}
  a{color:#722946;text-decoration:none;font-weight:650}nav a{border-radius:8px;padding:6px 10px}nav a[aria-current=page]{color:white;background:#662440}
</style>
