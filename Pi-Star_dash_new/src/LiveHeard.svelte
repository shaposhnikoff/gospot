<script lang="ts">
  import { onMount } from 'svelte';
  const fields = ['time_utc', 'mode', 'callsign', 'target', 'src', 'duration', 'loss', 'bit_error_rate', 'rssi'] as const;
  const heads = ['Время UTC', 'Режим', 'Позывной', 'Адресат', 'Вход', 'Длительность', 'Потери', 'BER', 'RSSI'];
  let rows = $state<Record<string,string>[]>([]);
  let error = $state(false), loaded = $state(false);
  let stamp = $state('');
  onMount(() => {
    const ctrl = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function update() {
      try {
        const res = await fetch('/api/last_heard.php?num_transmissions=20', {
          headers: { Accept: 'application/json' }, cache: 'no-store', mode: 'same-origin',
          signal: AbortSignal.any([ctrl.signal, AbortSignal.timeout(10000)])
        });
        if (!res.ok) throw new Error();
        const data: unknown = await res.json();
        if (!Array.isArray(data) || !data.every(x => x && typeof x === 'object' && fields.every(k => typeof x[k] === 'string'))) throw new Error();
        rows = data.slice(0,20); loaded = true; error = false; stamp = new Date().toLocaleTimeString('ru-RU');
      } catch { if (!ctrl.signal.aborted) error = true; }
      finally { if (!ctrl.signal.aborted) timer = setTimeout(update, 5000); }
    }
    void update(); return () => { ctrl.abort(); clearTimeout(timer); };
  });
</script>
<section class="panel feed"><h2>Последние вызовы · GoSpot API</h2>
  {#if stamp}<small>Получено: {stamp}</small>{/if}
  {#if error}<p role="status">Не удалось прочитать журнал.{loaded ? ' Показан последний снимок.' : ''}</p>{/if}
  {#if rows.length}<div class="table-scroll"><table><thead><tr>{#each heads as h}<th>{h}</th>{/each}</tr></thead>
  <tbody>{#each rows as row}<tr>{#each fields as k}<td>{row[k] || '—'}</td>{/each}</tr>{/each}</tbody></table></div>
  {:else if !error}<p>{loaded ? 'В журнале нет вызовов.' : 'Читаем журнал…'}</p>{/if}
</section>
<style>.feed{margin-block:1rem;padding:1rem}.feed small{display:block;margin:.5rem 0}</style>
