<script lang="ts">
  // admin/expert/modem_fw_upgrade.php: choose the modem type and follow the flash log.
  import { onMount } from 'svelte';
  let csrf = '';
  let options = $state<{ value: string; label: string }[] | null>(null);
  let modem = $state('');
  let started = $state(false);
  let running = $state(false);
  let error = $state('');
  let text = $state('Starting FW Upgrade, please wait...\n');
  let tail = $state<HTMLDivElement>();
  let offset = 0;

  async function poll() {
    const r = await fetch(`/manage/api/expert/fw?offset=${offset}`, { cache: 'no-store' });
    if (!r.ok) { error = (await r.text()).trim(); return; }
    const d = await r.json();
    csrf = d.csrf; options = d.options; running = d.running; offset = d.offset;
    if (running) started = true;
    if (started && d.data && tail) {
      const atBottom = tail.scrollHeight - tail.clientHeight <= tail.scrollTop + 1;
      text += d.data;
      if (atBottom) queueMicrotask(() => { if (tail) tail.scrollTop = tail.scrollHeight; });
    }
  }
  onMount(() => {
    let stop = false; let timer: ReturnType<typeof setTimeout>;
    const loop = async () => { try { await poll(); } catch { /* retry */ } if (!stop) timer = setTimeout(loop, 1000); };
    void loop();
    return () => { stop = true; clearTimeout(timer); };
  });

  async function upgrade() {
    if (!modem) return;
    if (!confirm('This will flash the modem firmware. Are you sure?')) return;
    const r = await fetch('/manage/api/expert/fw', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-PiStar-CSRF': csrf }, body: JSON.stringify({ modem }),
    }).catch(() => null);
    if (!r || r.status !== 202) { error = r ? (await r.text()).trim() : 'No response from the board'; return; }
    offset = 0; text = 'Starting FW Upgrade, please wait...\n'; started = true;
  }
</script>

<table width="100%">
  <tbody>
    {#if !started}
      <tr><th>Modem Firmware Upgrade Utility</th></tr>
      <tr><td align="center" style="white-space: normal;">
        {#if error}<p style="color:#b00;">{error}</p>{/if}
        {#if options}
          <p>
            <label for="modem">Select Modem:</label>
            <select id="modem" bind:value={modem}>
              <option value="" disabled>Please choose device type...</option>
              {#each options as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
            </select>
            <input type="button" value="Upgrade Modem" onclick={upgrade} />
          </p>
        {/if}
      </td></tr>
    {:else}
      <tr><th>Modem Flash/Upgrade Output</th></tr>
      <tr><td align="left"><div id="tail" bind:this={tail}>{text}</div></td></tr>
    {/if}
  </tbody>
</table>
