<script lang="ts">
  // One D-Star partial of the classic dashboard: dstarrepeater/
  // gateway_software_config.php, active_reflector_links.php,
  // css_connections.php (+ active_starnet_groups.php), last_herd.php or
  // local_tx.php (Pi-Star_DV_Dash 10bf34c9).
  import { type DStarInfo, type DStarHeardRow, nbsp, flagStyle, dstarCall, starNetHref } from './dstar';
  import red from './images/20red.png';

  let { info, section, timeZone = '', lookup = 'RadioID' }: {
    info: DStarInfo; section: 'gateway' | 'links' | 'ccs' | 'heard' | 'local'; timeZone?: string; lookup?: string;
  } = $props();
</script>

{#snippet headers(title: string, rows: DStarHeardRow[] | null)}
  <b>{title}</b>
  <table>
    <tbody>
      <tr>
        <th><a class="tooltip" href="#top">Time ({timeZone})</a></th>
        <th><a class="tooltip" href="#top">Callsign</a></th>
        <th><a class="tooltip" href="#top">Target</a></th>
        <th><a class="tooltip" href="#top">RPT 1</a></th>
        <th><a class="tooltip" href="#top">RPT 2</a></th>
      </tr>
      {#each rows ?? [] as r}
        {@const c = dstarCall(r, lookup)}
        <tr>
          <td align="left">{r.Clock}</td>
          <td align="left" width="180"><div style="float:left;"><a href={c.href} target="_blank" rel="noopener noreferrer">{c.text}</a>{#if c.id}/{c.id}{/if}</div> <div style="text-align:right;">(<a href={c.dprs} target="_blank" rel="noopener noreferrer">dPRS</a>)</div></td>
          <td align="left" width="100">{nbsp(r.Your)}</td>
          <td align="left" width="100">{nbsp(r.RPT1)}</td>
          <td align="left" width="100">{nbsp(r.RPT2)}</td>
        </tr>
      {/each}
    </tbody>
  </table>
{/snippet}

{#if section === 'gateway' && info.Gateway}
  <table>
    <tbody>
      <tr><th>ircDDB Network</th><th>APRS Host</th>{#each info.Gateway.Flags ?? [] as f}<th>{f.label}</th>{/each}</tr>
      <tr style="background: #ffffff;">
        <td>{info.Gateway.IRCDDB}</td>
        <td>{#if info.Gateway.APRS}{info.Gateway.APRS}{:else}<img src={red} alt="" />{/if}</td>
        {#each info.Gateway.Flags ?? [] as f}<td style={flagStyle(f.state)}>{f.state === 'on' ? 'ON' : 'OFF'}</td>{/each}
      </tr>
    </tbody>
  </table>
{:else if section === 'links'}
  <b>D-Star Link Information</b>
  <table>
    <tbody>
      <tr>
        <th><a class="tooltip" href="#top">Radio<span><b>Radio Module</b></span></a></th>
        <th><a class="tooltip" href="#top">Default<span><b>Default Link Destination</b></span></a></th>
        <th><a class="tooltip" href="#top">Auto<span><b>AutoLink</b>- green: enabled<br />- red: disabled</span></a></th>
        <th><a class="tooltip" href="#top">Timer<span><b>Reset/Restart Timer</b></span></a></th>
        <th><a class="tooltip" href="#top">Link<span><b>Link-Status</b>- green: enabled<br />- red: disabled</span></a></th>
        <th><a class="tooltip" href="#top">Linked to<span><b>Linked Destination</b></span></a></th>
        <th><a class="tooltip" href="#top">Mode<span><b>Mode or Protocol used</b></span></a></th>
        <th><a class="tooltip" href="#top">Direction<span><b>Direction</b>Incoming or Outgoing</span></a></th>
        <th><a class="tooltip" href="#top">Last Change ({timeZone})<span><b>Timestamp of last change</b>Time of last change in {timeZone} time zone</span></a></th>
      </tr>
      {#each info.Links ?? [] as l}
        <tr>
          <td>{nbsp(l.Radio)}</td>
          {#if l.Incoming}
            <td>&nbsp;</td><td>&nbsp;</td><td>&nbsp;</td>
          {:else}
            <td>{nbsp(l.Default) || ' '}</td><td>{l.Auto}</td><td>{l.Timer || ' '}</td>
          {/if}
          <td>{l.Status}</td>
          <td>{nbsp(l.Linked)}</td>
          <td>{l.Mode}</td>
          <td>{l.Direction}</td>
          <td>{l.Changed}</td>
        </tr>
      {/each}
    </tbody>
  </table>
{:else if section === 'ccs'}
  {#if info.CCSShow}
    <b>Active CCS Connections</b>
    <table>
      <tbody>
        <tr>
          <th><a class="tooltip" href="#top">Repeater<span><b>Callsign of connected repeater</b></span></a></th>
          <th><a class="tooltip" href="#top">Linked to<span><b>Actual link status</b></span></a></th>
          <th><a class="tooltip" href="#top">Protocol<span><b>Protocol</b></span></a></th>
          <th><a class="tooltip" href="#top">Direction<span><b>Direction</b>incoming or outgoing</span></a></th>
          <th><a class="tooltip" href="#top">Last Change ({timeZone})<span><b>Timestamp of last change</b>{timeZone}</span></a></th>
        </tr>
        {#each info.CCS ?? [] as c}
          <tr><td>{c.Repeater}</td><td>{c.Remote}</td><td>CCS</td><td>{c.Direction}</td><td>{c.Changed}</td></tr>
        {/each}
      </tbody>
    </table>
    <br />
  {/if}
  {#if info.StarNet}
    <b>Active Starnet Groups</b>
    <table style="table-layout: fixed;">
      <tbody>
        <tr>
          <th><a class="tooltip" href="#top">Callsign<span><b>Starnet Callsign</b></span></a></th>
          <th><a class="tooltip" href="#top">LogOff<span><b>Starnet Logoff Callsign</b></span></a></th>
          <th colspan="3"><a class="tooltip" href="#top">Information<span><b>Infotext</b></span></a></th>
          <th><a class="tooltip" href="#top">UTOT<span><b>User TimeOut (min)</b>inactivity time after which a user will be disconnected</span></a></th>
          <th><a class="tooltip" href="#top">GTOT<span><b>Group TimeOut (min)</b>inactivity time after which the group will be disconnected</span></a></th>
        </tr>
        {#each info.StarNet.Groups ?? [] as g}
          <tr>
            <td align="center">{nbsp(g.Callsign)}</td>
            {#if g.Logoff}<td align="center">{nbsp(g.Logoff)}</td>{:else}<td>&nbsp;</td>{/if}
            {#if g.Info}<td colspan="3" align="left">{g.Info}</td>{:else}<td colspan="3">&nbsp;</td>{/if}
            {#if g.UserTimeout}<td align="center">{g.UserTimeout}</td>{:else}<td>&nbsp;</td>{/if}
            {#if g.GroupTimeout}<td align="center">{g.GroupTimeout}</td>{:else}<td>&nbsp;</td>{/if}
          </tr>
        {/each}
      </tbody>
    </table>
    <br />
    {#if info.StarNet.Members?.length}
      <b>Active Starnet Group Members</b>
      <table style="table-layout: fixed;">
        <tbody>
          <tr>
            <th><a class="tooltip" href="#top">Time ({timeZone})<span><b>Time of Login</b></span></a></th>
            <th><a class="tooltip" href="#top">Group<span><b>Starnet Callsign</b></span></a></th>
            <th><a class="tooltip" href="#top">Callsign<span><b>Callsign</b></span></a></th>
          </tr>
          {#each info.StarNet.Members as m}
            <tr>
              <td align="left">{m.Clock}</td>
              <td align="center">{nbsp(m.Group)}</td>
              <td align="center"><a href={starNetHref(m.Callsign)} target="_blank" rel="noopener noreferrer">{m.Callsign}</a></td>
            </tr>
          {/each}
        </tbody>
      </table>
      <br />
    {/if}
  {/if}
{:else if section === 'heard'}
  {@render headers('Gateway Activity', info.Heard)}
{:else if section === 'local'}
  {@render headers('Local RF Activity', info.Local)}
{/if}
