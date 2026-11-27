<script lang="ts">
  // admin/power.php: Reboot / Shutdown with confirmation and CSRF.
  import { onMount } from 'svelte';
  import GoSpotFrame from './GoSpotFrame.svelte';
  import { operatorOrigin } from './runtime';
  import reboot from './images/reboot.png';
  import shutdown from './images/shutdown.png';

  let csrf = '';
  let sent = $state<'reboot' | 'shutdown' | null>(null);
  let error = $state('');
  let countdown = $state(90);

  onMount(() => {
    void fetch('/manage/api/power', { cache: 'no-store' }).then(r => r.json()).then(d => { csrf = d.csrf; }).catch(() => {
      error = 'Could not reach the board.';
    });
  });

  async function power(action: 'reboot' | 'shutdown') {
    if (!confirm('Are you sure?')) return;
    error = '';
    const r = await fetch('/manage/api/power', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-GoSpot-CSRF': csrf }, body: JSON.stringify({ action }),
    }).catch(() => null);
    if (!r || r.status !== 202) {
      error = r ? (await r.text()).trim() : 'Could not reach the board.';
      return;
    }
    sent = action;
    if (action === 'reboot') {
      const target = (await operatorOrigin()) ?? '';
      const t = setInterval(() => {
        countdown--;
        if (countdown <= 0) { clearInterval(t); location.href = target + '/'; }
      }, 1000);
    }
  }
</script>

<GoSpotFrame title="Power" links={['Dashboard', 'Admin', 'Update', 'Backup/Restore', 'Configuration']}>
  <table width="100%">
    <tbody>
      <tr><th colspan="2">Power</th></tr>
      {#if sent === 'reboot'}
        <tr><td colspan="2" style="background: #000000; color: #00ff00;"><br /><br />Reboot command has been sent to your Pi,
          <br />please wait up to 90 secs for it to reboot.<br />
          <br />You will be re-directed back to the
          <br />dashboard automatically in <span id="countdown">{countdown}</span> seconds.<br /><br /><br /></td></tr>
      {:else if sent === 'shutdown'}
        <tr><td colspan="2" style="background: #000000; color: #00ff00;"><br /><br />Shutdown command has been sent to your Pi,
          <br /> please wait 30 secs for it to fully shutdown<br />before removing the power.<br /><br /><br /></td></tr>
      {:else}
        <tr>
          <td align="center">Reboot<br /><button class="icon" type="button" onclick={() => power('reboot')}><img src={reboot} alt="Reboot" /></button></td>
          <td align="center">Shutdown<br /><button class="icon" type="button" onclick={() => power('shutdown')}><img src={shutdown} alt="Shutdown" /></button></td>
        </tr>
        {#if error}<tr><td colspan="2" style="background:#f33;">{error}</td></tr>{/if}
      {/if}
    </tbody>
  </table>
</GoSpotFrame>
