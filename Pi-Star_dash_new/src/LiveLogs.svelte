<script lang="ts">
  // admin/live_modem_log.php: today's MMDVMHost log, new lines every second.
  import { onMount } from 'svelte';
  import PiStarFrame from './PiStarFrame.svelte';

  let text = $state('Starting logging, please wait...\n');
  let tail: HTMLDivElement;

  onMount(() => {
    let offset: number | null = null;
    let stop = false;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const r = await fetch('/manage/api/livelog' + (offset == null ? '' : `?offset=${offset}`), { cache: 'no-store' });
        if (r.ok) {
          const reply: { offset: number; data: string } = await r.json();
          offset = reply.offset;
          if (reply.data) {
            const atBottom = tail.scrollHeight - tail.clientHeight <= tail.scrollTop + 1;
            text += reply.data;
            if (atBottom) queueMicrotask(() => { tail.scrollTop = tail.scrollHeight; });
          }
        }
      } catch { /* retry on the next tick, like $.repeat */ }
      if (!stop) timer = setTimeout(poll, 1000);
    }
    void poll();
    return () => { stop = true; clearTimeout(timer); };
  });
</script>

<PiStarFrame title="Live Logs" links={['Dashboard', 'Admin', 'Power', 'Backup/Restore', 'Configuration']}>
  <table width="100%">
    <tbody>
      <tr><th>Live Logs</th></tr>
      <tr><td align="left"><div id="tail" bind:this={tail}>{text}</div></td></tr>
      <tr><th>Download the log: <a href="/manage/api/livelog/download" style="color: #ffffff;">here</a></th></tr>
    </tbody>
  </table>
</PiStarFrame>
