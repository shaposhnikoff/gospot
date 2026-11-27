<script lang="ts">
  // admin/update.php: start the update and follow its log. The native update
  // refreshes the Pi-Star host files and ID databases and upgrades the OS
  // packages; it never pulls a PHP dashboard.
  import { onMount } from 'svelte';
  import GoSpotFrame from './GoSpotFrame.svelte';
  import download from './images/download.png';

  let csrf = '';
  let running = $state(false);
  let started = $state(false);
  let error = $state('');
  let text = $state('Starting update, please wait...\n');
  let tail = $state<HTMLDivElement>();
  let offset = 0;

  async function poll() {
    const r = await fetch(`/manage/api/update?offset=${offset}`, { cache: 'no-store' });
    if (!r.ok) throw new Error(String(r.status));
    const d: { offset: number; data: string; running: boolean; csrf: string } = await r.json();
    csrf = d.csrf;
    running = d.running;
    offset = d.offset;
    if (d.data && tail) {
      const atBottom = tail.scrollHeight - tail.clientHeight <= tail.scrollTop + 1;
      text += d.data;
      if (atBottom) queueMicrotask(() => { if (tail) tail.scrollTop = tail.scrollHeight; });
    }
  }

  onMount(() => {
    let stop = false;
    let timer: ReturnType<typeof setTimeout>;
    const loop = async () => {
      try { await poll(); } catch { /* keep trying */ }
      if (running) started = true;
      if (!stop) timer = setTimeout(loop, 1000);
    };
    void loop();
    return () => { stop = true; clearTimeout(timer); };
  });

  async function start() {
    error = '';
    const r = await fetch('/manage/api/update', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-GoSpot-CSRF': csrf }, body: JSON.stringify({ confirm: true }),
    }).catch(() => null);
    if (!r || r.status !== 202) {
      error = r ? (await r.text()).trim() : 'Could not reach the board.';
      return;
    }
    offset = 0;
    text = 'Starting update, please wait...\n';
    started = true;
  }
</script>

<GoSpotFrame title="Update" links={['Dashboard', 'Admin', 'Power', 'Backup/Restore', 'Configuration']}>
  {#if started}
    <table width="100%">
      <tbody>
        <tr><th>{running ? 'Update Running' : 'Update Finished'}</th></tr>
        <tr><td align="left"><div id="tail" bind:this={tail}>{text}</div></td></tr>
      </tbody>
    </table>
  {:else}
    <table width="100%">
      <tbody>
        <tr><th>Update</th></tr>
        <tr>
          <td align="center" style="padding: 20px;">
            <p style="margin-bottom: 20px;">
              <strong>This will update the GoSpot host files, ID databases and system packages.</strong><br />
              <br />
              The update process may take several minutes to complete.<br />
              Please do not interrupt the process or power off your system during the update.
            </p>
            <button class="icon" type="button" onclick={start}>
              <img src={download} alt="Start Update" /><br />
              <strong>Start Update</strong>
            </button>
          </td>
        </tr>
        {#if error}<tr><td style="background:#f33;">{error}</td></tr>{/if}
      </tbody>
    </table>
  {/if}
</GoSpotFrame>
