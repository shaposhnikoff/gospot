<script lang="ts">
  // admin/calibration.php: MMDVMCal on the board's modem. Start holds
  // MMDVMHost and runs MMDVMCal; the buttons send its single-key commands;
  // Stop (or leaving the page) quits it and starts MMDVMHost again. Save
  // Offset writes [Modem] RXOffset of the MMDVMHost configuration.
  // #calibration-debug also shows MMDVMCal's output, like ?debug.
  import { onMount } from 'svelte';
  import GoSpotFrame from './GoSpotFrame.svelte';
  import red from './images/20red.png';
  import green from './images/20green.png';
  import { BerMeter, MODES, chart, offsetOf, scan, startFrequency, validOffset, type Figures, type Mode } from './calibration';

  let { debug = false }: { debug?: boolean } = $props();

  let csrf = '';
  let revision = '';
  let rxFrequency = $state(0);
  let rxOffset = 0;
  let frequency = $state('');
  let offset = $state('');
  let started = $state(false);
  let mode = $state<Mode | null>(null);
  let startDisabled = $state(false);
  let error = $state('');
  let every = $state(5);
  let current = $state<Figures | null>(null);
  let total = $state<Figures | null>(null);
  let points = $state<[number, number][]>([[0, 0]]);
  let text = $state('');
  let tail = $state<HTMLDivElement>();
  let loaded = $state(false);
  const meter = new BerMeter();
  const W = 780, H = 260;
  const plot = $derived(chart(points, W - 60, H - 70));

  async function post(body: Record<string, string>): Promise<Response | null> {
    return fetch('/manage/api/calibration', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-GoSpot-CSRF': csrf }, body: JSON.stringify(body),
    }).catch(() => null);
  }

  async function failed(r: Response | null) {
    error = r ? (await r.text()).trim() : 'Could not reach the board.';
  }

  async function sendaction(action: 'start' | 'saveoffset', param = '') {
    error = '';
    if (action === 'start') {
      startDisabled = true;
      const r = await post({ action });
      if (r?.status !== 202) { await failed(r); startDisabled = false; }
      return;
    }
    if (!validOffset(param)) { error = 'Invalid offset'; return; }
    const r = await post({ action, param: param.trim(), revision });
    if (r?.status !== 200) { await failed(r); return; }
    const d: { revision: string; rxOffset: number } = await r.json();
    revision = d.revision;
    rxOffset = d.rxOffset;
  }

  let lock = false;
  async function sendcmd(cmd: string, param = '') {
    if (lock) return;
    if (param !== '') lock = true; // no other key while MMDVMCal waits for the argument
    error = '';
    try {
      const r = await post({ action: 'cmd', cmd, param });
      if (r?.status !== 200) await failed(r);
    } finally {
      lock = false;
    }
  }

  function show(data: string) {
    if (debug && tail) {
      const atBottom = tail.scrollHeight - tail.clientHeight <= tail.scrollTop + 1;
      text += data.replace(/\r/g, '');
      if (atBottom) queueMicrotask(() => { if (tail) tail.scrollTop = tail.scrollHeight; });
    }
    const s = scan(data);
    if (s.version) setTimeout(() => void sendcmd('e', startFrequency(rxFrequency, rxOffset)), 1000);
    if (s.finished) { started = false; mode = null; startDisabled = false; }
    if (s.mode) { started = true; mode = s.mode; }
    if (s.frequency !== null) {
      started = true;
      frequency = `${s.frequency} Hz`;
      offset = offsetOf(s.frequency, rxFrequency);
    }
    meter.add(s);
  }

  onMount(() => {
    let stop = false;
    let timer: ReturnType<typeof setTimeout>;
    let at: number | null = null;
    const loop = async () => {
      try {
        if (at === null) {
          const r = await fetch('/manage/api/calibration', { cache: 'no-store' });
          if (!r.ok) throw new Error((await r.text()).trim());
          const d: { csrf: string; revision: string; rxFrequency: number; rxOffset: number; offset: number; running: boolean } = await r.json();
          csrf = d.csrf; revision = d.revision; rxFrequency = d.rxFrequency; rxOffset = d.rxOffset; at = d.offset;
          frequency = `${d.rxFrequency + d.rxOffset} Hz`;
          offset = String(d.rxOffset);
          startDisabled = d.running;
          loaded = true;
        } else {
          const r = await fetch(`/manage/api/calibration?offset=${at}`, { cache: 'no-store' });
          if (r.ok) {
            const d: { csrf: string; offset: number; data: string; running: boolean } = await r.json();
            csrf = d.csrf;
            at = d.offset;
            if (d.data) show(d.data);
            if (meter.tick(every)) { current = meter.current; total = meter.total; points = meter.points; }
          }
        }
      } catch (e) {
        if (!loaded) error = (e as Error).message || 'Could not read the MMDVMHost configuration';
      }
      if (!stop) timer = setTimeout(loop, 1000);
    };
    void loop();
    return () => { stop = true; clearTimeout(timer); };
  });

  const led = (on: boolean) => (on ? green : red);
</script>

<GoSpotFrame title="Calibration" links={['Dashboard', 'Admin', 'Power', 'Backup/Restore', 'Configuration']}>
  <table width="100%">
    <tbody>
      <tr><th>Calibration Tool</th></tr>
      {#if error}<tr><td style="background:#f33;">{error}</td></tr>{/if}
      <tr><td align="left">
        <table width="800" border="0" cellspacing="0" cellpadding="5">
          <tbody>
            <tr>
              <td align="center" valign="middle"><table border="0" cellspacing="0" cellpadding="5"><tbody>
                <tr>
                  <td><input type="button" id="btnStart" value="Start" disabled={startDisabled || !loaded} onclick={() => sendaction('start')} /></td>
                  <td><img src={led(started)} id="ledStart" width="20" height="20" alt={started ? 'running' : 'stopped'} /></td>
                </tr>
                <tr>
                  <td><input type="button" id="btnStop" value="Stop" onclick={() => sendcmd('q')} /></td>
                  <td>&nbsp;</td>
                </tr>
              </tbody></table></td>

              <td align="center" valign="middle"><table border="0" cellspacing="0" cellpadding="4" style="height:160px;"><tbody>
                {#each MODES as [id, label, key] (id)}
                  <tr>
                    <td><input type="button" id={'btn' + id} value={label} onclick={() => sendcmd(key)} /></td>
                    <td><img src={led(mode === id)} id={'led' + id} width="20" height="20" alt={mode === id ? label + ' on' : label + ' off'} /></td>
                  </tr>
                {/each}
              </tbody></table></td>

              <td align="center" valign="middle"><table border="0" cellspacing="0" cellpadding="5" style="height:160px;"><tbody>
                <tr>
                  <td align="left">Base Freq.:</td>
                  <td colspan="3" id="lblBaseFreq">{rxFrequency} Hz</td>
                </tr>
                <tr>
                  <td align="left">Frequency:</td>
                  <td colspan="3" id="lblFrequency">{frequency}</td>
                </tr>
                <tr>
                  <td align="left">Offset:</td>
                  <td><input type="button" id="btnFreqM" value="-" onclick={() => sendcmd('f')} /></td>
                  <td id="lblOffset" style="width:5ch">{offset}</td>
                  <td><input type="button" id="btnFreqP" value="+" onclick={() => sendcmd('F')} /></td>
                </tr>
                <tr>
                  <td align="left">Step:</td>
                  <td colspan="3">{#each ['25', '50', '100'] as step}<input type="button" value={step} onclick={() => sendcmd('z', step)} />{' '}{/each}</td>
                </tr>
                <tr>
                  <td align="left">&nbsp;</td>
                  <td colspan="3"><input type="button" id="btnSaveOffset" value="Save Offset" onclick={() => sendaction('saveoffset', offset)} /></td>
                </tr>
              </tbody></table></td>

              <td align="center" valign="middle"><table border="0" cellspacing="0" cellpadding="5" style="height:160px;"><tbody>
                <tr><th style="width:8ch">&nbsp;</th><th style="width:9ch">Current</th><th style="width:9ch">Total</th></tr>
                <tr><td align="left">Frames:</td><td id="lblFrames">{current?.frames ?? ''}</td><td id="lblTFrames">{total?.frames ?? ''}</td></tr>
                <tr><td align="left">Bits:</td><td id="lblBits">{current?.bits ?? ''}</td><td id="lblTBits">{total?.bits ?? ''}</td></tr>
                <tr><td align="left">Errors:</td><td id="lblErrors">{current?.errors ?? ''}</td><td id="lblTErrors">{total?.errors ?? ''}</td></tr>
                <tr><td align="left">BER:</td><td id="lblBER">{current?.ber ?? ''}</td><td id="lblTBER">{total?.ber ?? ''}</td></tr>
                <tr>
                  <td align="left">Seconds:</td>
                  <td id="lblSec" style="padding:0;">
                    <select id="sltUpdFrq" style="margin:0;" bind:value={every} aria-label="Update every">
                      {#each [1, 2, 3, 5, 10, 30] as n}<option value={n}>{n}</option>{/each}
                    </select>
                  </td>
                  <td id="lblTSec">{total?.seconds ?? ''}</td>
                </tr>
              </tbody></table></td>
            </tr>
          </tbody>
        </table>
      </td></tr>
      <tr><td align="left">
        <!-- The classic page draws this with Plotly: BER %, 0..5, last three minutes. -->
        <svg id="chart" viewBox={`0 0 ${W} ${H}`} width="100%" role="img" aria-label="Bit Error Rate (BER)">
          <rect width={W} height={H} fill="#ffffff" />
          <text x={W / 2} y="22" text-anchor="middle" font-size="16">Bit Error Rate (BER)</text>
          <g transform="translate(45 35)">
            {#each [0, 1, 2, 3, 4, 5] as v}
              <line x1="0" x2={W - 60} y1={(H - 70) * (1 - v / 5)} y2={(H - 70) * (1 - v / 5)} stroke="#eeeeee" />
              <text x="-6" y={(H - 70) * (1 - v / 5) + 4} text-anchor="end" font-size="11">{v}</text>
            {/each}
            <path d={plot.area} fill="rgba(221, 75, 57, 0.5)" />
            <path d={plot.line} fill="none" stroke="#dd4b39" stroke-width="2" />
            <text x="0" y={H - 52} font-size="11">{plot.xmin}</text>
            <text x={W - 60} y={H - 52} text-anchor="end" font-size="11">{plot.xmax}</text>
            <text x={(W - 60) / 2} y={H - 45} text-anchor="middle" font-size="12">Seconds</text>
            <text x="-32" y={(H - 70) / 2} text-anchor="middle" font-size="12" transform={`rotate(-90 -32 ${(H - 70) / 2})`}>%</text>
          </g>
        </svg>
      </td></tr>
      {#if debug}<tr><td align="left"><div id="tail" bind:this={tail}>{text}</div></td></tr>{/if}
    </tbody>
  </table>
</GoSpotFrame>

<style>
  #chart { display: block; }
</style>
