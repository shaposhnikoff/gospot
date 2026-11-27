<script lang="ts">
  // admin/expert/jitter_test.php?group=brandmeister|dmrplus|hblink (DMR+
  // without a group): opening the page runs gospot-jittertest for that host
  // group and follows its log. A run already in progress is followed instead.
  import { onMount } from 'svelte';

  let { group = '' }: { group?: string } = $props();
  let text = $state('Starting test, please wait...\n');
  let error = $state('');
  let tail = $state<HTMLDivElement>();

  onMount(() => {
    let stop = false;
    let timer: ReturnType<typeof setTimeout>;
    let offset = 0;
    const poll = async () => {
      try {
        const r = await fetch(`/manage/api/expert/jitter?offset=${offset}`, { cache: 'no-store' });
        if (r.ok) {
          const d: { offset: number; data: string } = await r.json();
          offset = d.offset;
          if (d.data && tail) {
            const atBottom = tail.scrollHeight - tail.clientHeight <= tail.scrollTop + 1;
            text += d.data;
            if (atBottom) queueMicrotask(() => { if (tail) tail.scrollTop = tail.scrollHeight; });
          }
        }
      } catch { /* retry on the next tick, like $.repeat */ }
      if (!stop) timer = setTimeout(poll, 1000);
    };
    void (async () => {
      try {
        const r = await fetch('/manage/api/expert/jitter', { cache: 'no-store' });
        if (!r.ok) throw new Error((await r.text()).trim());
        const { csrf }: { csrf: string } = await r.json();
        const s = await fetch('/manage/api/expert/jitter', {
          method: 'POST', headers: { 'Content-Type': 'application/json', 'X-GoSpot-CSRF': csrf }, body: JSON.stringify({ group }),
        });
        if (s.status !== 202 && s.status !== 409) throw new Error((await s.text()).trim());
      } catch (e) {
        error = (e as Error).message || 'Could not reach the board.';
      }
      if (!stop) void poll();
    })();
    return () => { stop = true; clearTimeout(timer); };
  });
</script>

<table width="100%">
  <tbody>
    <tr><th>Test Running</th></tr>
    {#if error}<tr><td style="background:#f33;">{error}</td></tr>{/if}
    <!-- The classic log is wrapped in <pre>: keep its tab-aligned columns. -->
    <tr><td align="left"><div id="tail" style="white-space: pre;" bind:this={tail}>{text}</div></td></tr>
  </tbody>
</table>
