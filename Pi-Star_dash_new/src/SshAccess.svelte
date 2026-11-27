<script lang="ts">
  // admin/expert/ssh_access.php: the shellinabox web client of this host.
  import { onMount } from 'svelte';
  let port = $state<number | null>(null);
  onMount(() => { void fetch('/manage/api/expert/ssh').then(r => r.json()).then(d => { port = d.port; }).catch(() => { port = 0; }); });
  const url = $derived(port ? `http://${location.hostname}:${port}` : '');
</script>

<table width="100%">
  <tbody>
    <tr><th>SSH - Pi-Star</th></tr>
    <tr><td align="left"><div id="tail">
      {#if url}
        <iframe src={url} title="Pi-Star_SSH" style="border:0px #ffffff none; background:#ffffff; color:#00ff00;" height="100%" width="100%"></iframe>
      {:else if port === 0}
        SSH Feature not yet installed
      {/if}
    </div></td></tr>
  </tbody>
</table>
{#if url}<a href={url}>Click here for fullscreen SSH client</a><br />{/if}
