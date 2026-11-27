<script lang="ts">
  // admin/expert/: header-menu.inc, the warning on index.php and the generic
  // Quick Edit editors (edit_*.php).
  import PiStarFrame from './PiStarFrame.svelte';
  import QuickEdit from './QuickEdit.svelte';
  import FullEdit from './FullEdit.svelte';
  import ApiKeys from './ApiKeys.svelte';
  import Firmware from './Firmware.svelte';
  import SshAccess from './SshAccess.svelte';
  import CssTool from './CssTool.svelte';
  import Jitter from './Jitter.svelte';

  let { page = '' }: { page?: string } = $props();
  const quick: [string, string][] = [
    ['dstarrepeater', 'DStarRepeater'], ['ircddbgateway', 'ircDDBGateway'], ['timeserver', 'TimeServer'],
    ['mmdvmhost', 'MMDVMHost'], ['dmrgateway', 'DMR GW'], ['ysfgateway', 'YSF GW'], ['p25gateway', 'P25 GW'],
    ['nxdngateway', 'NXDN GW'], ['dapnetgateway', 'DAPNET GW'],
  ];
  const full: [string, string][] = [
    ['full-dmrgateway', 'DMR GW'], ['full-pistar-remote', 'PiStar-Remote'], ['full-wpaconfig', 'WiFi'],
    ['full-apikeys', 'API Keys'], ['full-cron', 'System Cron'], ['full-rssidat', 'RSSI Dat'],
  ];
  const tools: [string, string][] = [['tool-css', 'CSS Tool'], ['tool-fw', 'FW'], ['tool-ssh', 'SSH Access']];
  const quickFile = $derived(quick.some(([f]) => f === page) ? page : null);
  // jitter_test.php has no menu entry in Pi-Star either; it is opened by its
  // address: #expert-tool-jitter[-brandmeister|-dmrplus|-hblink].
  const jitterGroup = $derived(page === 'tool-jitter' ? ''
    : ['brandmeister', 'dmrplus', 'hblink'].find(g => page === 'tool-jitter-' + g) ?? null);
  const fullFile = $derived(full.some(([f]) => f === page) && page !== 'full-apikeys' ? page.slice(5) : null);
</script>

{#snippet links(items: [string, string][])}
  {#each items as [id, label], i}<a href={'#expert-' + id} style="color: #ffffff;">{label}</a>{#if i < items.length - 1}{' | '}{/if}{/each}
{/snippet}

{#snippet menu()}
  <p style="padding-right: 5px; text-align: right; color: #ffffff;">
    <b>Quick Edit:</b>
    {@render links(quick)}<br />
    <b>Full Edit:</b>
    {@render links(full)}
    &emsp;<b>Tools:</b>
    {@render links(tools)}
  </p>
{/snippet}

<PiStarFrame title="Expert Editors" links={['Dashboard', 'Admin', 'Update', 'Backup/Restore', 'Configuration']} {menu} footer="dashboard">
  {#if quickFile}
    {#key quickFile}<QuickEdit file={quickFile} />{/key}
  {:else if fullFile}
    {#key fullFile}<FullEdit file={fullFile} />{/key}
  {:else if page === 'full-apikeys'}
    <ApiKeys />
  {:else if page === 'tool-fw'}
    <Firmware />
  {:else if page === 'tool-ssh'}
    <SshAccess />
  {:else if page === 'tool-css'}
    <CssTool />
  {:else if jitterGroup !== null}
    {#key jitterGroup}<Jitter group={jitterGroup} />{/key}
  {:else}
    <table width="100%">
      <tbody>
        <tr><th>Expert Editors</th></tr>
        <tr><td align="center" style="white-space: normal;">
          <h2>**WARNING**</h2>
          Pi-Star Expert editors have been created to make editing some of the extra settings in the<br />
          config files more simple, allowing you to update some areas of the config files without the<br />
          need to login to your Pi over SSH.<br />
          <br />
          Please keep in mind when making your edits here, that these config files can be updated by<br />
          the dashboard, and that your edits can be over-written. It is assumed that you already know<br />
          what you are doing editing the files by hand, and that you understand what parts of the files<br />
          are maintained by the dashboard.<br />
          <br />
          With that warning in mind, you are free to make any changes you like, for help come to the Facebook<br />
          group (link at the bottom of the page) and ask for help if / when you need it.<br />
          73 and enjoy your Pi-Star experiance.<br />
          Pi-Star UK Team.<br />
          <br />
        </td></tr>
      </tbody>
    </table>
  {/if}
</PiStarFrame>
