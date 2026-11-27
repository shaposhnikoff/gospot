<script lang="ts">
  // admin/config_backup.php: classic-compatible Pi-Star configuration zip.
  import { onMount } from 'svelte';
  import GoSpotFrame from './GoSpotFrame.svelte';
  import download from './images/download.png';
  import restoreIcon from './images/restore.png';

  let csrf = '';
  let file = $state<File | null>(null);
  let output = $state<string[] | null>(null);
  let ok = $state(false);
  let busy = $state(false);

  onMount(() => {
    void fetch('/manage/api/backup/classic?csrf=1', { cache: 'no-store' }).then(r => r.json()).then(d => { csrf = d.csrf; }).catch(() => {});
  });

  async function restore() {
    if (!file) { output = ['Please choose a .zip file to restore.']; ok = false; return; }
    if (!confirm('This will overwrite the current configuration with the contents of ' + file.name + '. Continue?')) return;
    busy = true;
    const r = await fetch('/manage/api/backup/classic', {
      method: 'POST', headers: { 'Content-Type': 'application/zip', 'X-GoSpot-CSRF': csrf }, body: await file.arrayBuffer(),
    }).catch(() => null);
    busy = false;
    if (!r) { output = ['No response from the board.']; ok = false; return; }
    if (!r.ok) { output = [(await r.text()).trim()]; ok = false; return; }
    const d: { ok: boolean; output: string[] } = await r.json();
    output = d.output; ok = d.ok;
  }
</script>

<GoSpotFrame title="Backup/Restore" links={['Dashboard', 'Admin', 'Power', 'Update', 'Configuration']}>
  {#if output}
    <table width="100%">
      <tbody>
        <tr><th colspan="2">Config Restore</th></tr>
        <tr><td align="left" style={ok ? '' : 'background:#f33;'}><pre>{output.join('\n')}</pre></td></tr>
      </tbody>
    </table>
    <p><a href="#backup" onclick={() => { output = null; }}>Back</a></p>
  {:else}
    <table width="100%">
      <tbody>
        <tr><th colspan="2">Backup/Restore</th></tr>
        <tr>
          <td align="center" valign="top" style="width:50%">Download Configuration<br />
            <a href="/manage/api/backup/classic" download><img src={download} alt="Download Config" /></a>
          </td>
          <td align="center" valign="top">Restore Configuration<br />
            <button class="icon" type="button" disabled={busy} onclick={restore}><img src={restoreIcon} alt="Restore Config" /></button><br />
            <input type="file" accept=".zip,application/zip" onchange={e => { file = (e.currentTarget as HTMLInputElement).files?.[0] ?? null; }} aria-label="Configuration archive" />
          </td>
        </tr>
        <tr>
          <td colspan="2" align="justify" style="white-space: normal;">
            <br />
            <b>WARNING:</b><br />
            Editing the files outside of GoSpot *could* have un-desireable side effects.<br />
            <br />
            This backup and restore tool, will backup your config files to a Zip file, and allow you to restore them later<br />
            either to this GoSpot or another one.<br />
            <ul>
              <li>System Passwords / Dashboard passwords are NOT backed up / restored.</li>
              <li>Wireless Configuration IS backed up and restored</li>
            </ul>
          </td>
        </tr>
      </tbody>
    </table>
  {/if}
</GoSpotFrame>
