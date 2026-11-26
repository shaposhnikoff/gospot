<script lang="ts">
  import { searchGroups } from './routes';

  const links = [
    ['/', 'Обзор узла'],
    ['/admin/', 'Администрирование'],
    ['/admin/configure.php', 'Настройки'],
    ['/admin/config_backup.php', 'Резервная копия'],
    ['/admin/live_modem_log.php', 'Живой журнал'],
  ];
  const pathname = location.pathname.replace(/\/index\.php$/, '/');
  let query = $state('');
  const results = $derived(query.trim() ? searchGroups(query, '').flatMap(group =>
    group.routes.filter(route => route.path)) : []);
</script>

<nav lang="ru" aria-label="Панель оператора Pi-Star">
  <div class="bar">
    <a class="brand" href="/operator/">Pi-Star <span>· Оператор</span></a>
    <div class="links">
      {#each links as [path, label]}
        <a href={path} aria-current={pathname === path ? 'page' : undefined}>{label}</a>
      {/each}
    </div>
    <details>
      <summary>Поиск функций</summary>
      <div class="panel">
        <label for="search">Найти функцию Pi-Star</label>
        <input id="search" type="search" bind:value={query} placeholder="Wi-Fi, DMR, журнал…" />
        {#if query.trim()}
          <small role="status">Найдено: {results.length}</small>
          <ul>
            {#each results as route (route.id)}
              <li><a href={route.path}>{route.title}</a></li>
            {/each}
          </ul>
        {/if}
        <a class="catalog" href="/operator/#console">Открыть весь каталог →</a>
      </div>
    </details>
  </div>
</nav>

<style>
  nav { color: #f8f5f7; background: #272331; font: 500 .92rem/1.4 system-ui, sans-serif; text-align: left; }
  .bar { max-width: 1240px; margin: auto; padding: .55rem 1rem; display: flex; align-items: center; flex-wrap: wrap; gap: .4rem 1rem; }
  .links { display: flex; flex-wrap: wrap; gap: .15rem; }
  a, summary { display: inline-block; padding: .55rem .65rem; border-radius: .4rem; color: inherit; text-decoration: none; cursor: pointer; }
  a:hover, summary:hover, a[aria-current] { background: #514051; text-decoration: underline; text-underline-offset: .2em; }
  :focus-visible { outline: 3px solid #e391b4; outline-offset: 1px; }
  .brand { font-weight: 750; }
  .brand span { color: #f1bad1; }
  details { margin-inline-start: auto; position: relative; }
  summary { display: list-item; }
  .panel { box-sizing: border-box; position: absolute; z-index: 5; inset-inline-end: 0; width: min(22rem, 90vw); padding: 1rem; background: white; border: 1px solid #d8cfda; border-radius: .6rem; box-shadow: 0 .45rem 1.4rem #21162626; color: #312936; }
  label, input, small { display: block; }
  input { width: 100%; box-sizing: border-box; font: inherit; color: inherit; background: white; margin: .4rem 0; padding: .6rem; border: 1px solid #937d89; border-radius: .35rem; }
  ul { margin: .3rem 0; padding: 0; list-style: none; max-height: 35vh; overflow-y: auto; }
  .panel a { display: block; color: #87204b; }
  .panel a:hover, .panel a:focus-visible { background: #f6eff3; }
  .catalog { font-weight: 700; }
  @media (max-width: 60rem) {
    .bar { gap: 0; }
    .links { order: 1; width: 100%; }
    .links a { padding: .5rem; }
  }
</style>
