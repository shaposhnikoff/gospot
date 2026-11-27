<script lang="ts">
  // BrandMeister Hoseline, listen only: one talkgroup relayed by this board
  // (panel/hoseline.go), μ-law decoded here and played through an AudioWorklet.
  import { onMount } from 'svelte';
  import workletURL from './hoseline-worklet.ts?worker&url';
  import { callsign } from './classic';
  import { apiBase, decodeMulaw, parseEvent, parseGroups, refusal, socketURL } from './hoseline';

  let { admin = false, lookup = 'RadioID' }: { admin?: boolean; lookup?: string } = $props();

  let groups = $state<number[] | null>(null);
  let tg = $state('');
  let phase = $state<'idle' | 'connecting' | 'listening' | 'error'>('idle');
  let listening = $state(0);
  let caller = $state<number | null>(null);
  let message = $state('');
  let socket: WebSocket | null = null;
  let audio: AudioContext | null = null;
  let node: AudioWorkletNode | null = null;
  let session = 0;

  async function readGroups() {
    const r = await fetch(apiBase(admin) + '/groups', { cache: 'no-store', mode: 'same-origin', signal: AbortSignal.timeout(8000) });
    if (!r.ok) throw new Error(String(r.status));
    return parseGroups(await r.json());
  }

  onMount(() => {
    readGroups().then(g => {
      groups = g?.groups ?? [];
      if (groups.length) tg = String(groups.includes(91) ? 91 : groups[0]);
    }).catch(() => { groups = []; });
    return () => stop();
  });

  function release() {
    socket?.close(1000);
    socket = null;
    node?.port.postMessage({ type: 'reset' });
    node?.disconnect();
    node = null;
    void audio?.close().catch(() => {});
    audio = null;
  }

  function fail(mine: number, text: string) {
    if (mine !== session) return;
    release();
    phase = 'error';
    caller = null;
    message = text;
  }

  // Play must come from a click: the AudioContext is created and resumed
  // before the first await, inside the user gesture.
  async function play() {
    const group = Number(tg);
    if (!Number.isInteger(group) || group <= 0) return;
    release();
    const mine = ++session;
    phase = 'connecting';
    listening = group;
    caller = null;
    try {
      audio = new AudioContext({ sampleRate: 8000 });
    } catch {
      audio = new AudioContext(); // the worklet resamples
    }
    const ctx = audio;
    void ctx.resume().catch(() => {});
    try {
      await ctx.audioWorklet.addModule(workletURL);
    } catch {
      fail(mine, 'Audio playback unavailable in this browser');
      return;
    }
    if (mine !== session) return;
    node = new AudioWorkletNode(ctx, 'hoseline-audio', { numberOfInputs: 0, outputChannelCount: [1] });
    node.connect(ctx.destination);
    const out = node;
    const ws = new WebSocket(socketURL(location, admin, group));
    ws.binaryType = 'arraybuffer';
    socket = ws;
    let opened = false;
    ws.onopen = () => { opened = true; };
    ws.onmessage = (e: MessageEvent) => {
      if (mine !== session) return;
      if (e.data instanceof ArrayBuffer) {
        const samples = decodeMulaw(new Uint8Array(e.data));
        out.port.postMessage({ type: 'audio', samples }, [samples.buffer]);
        return;
      }
      const ev = typeof e.data === 'string' ? parseEvent(e.data) : null;
      if (!ev) return;
      if (ev.type === 'listening') { phase = 'listening'; listening = ev.tg; }
      else if (ev.type === 'start') caller = ev.source;
      else if (ev.type === 'end') { caller = null; out.port.postMessage({ type: 'flush' }); }
      else fail(mine, ev.message);
    };
    ws.onclose = () => {
      if (mine !== session || phase === 'error') return;
      if (opened) { fail(mine, 'Hoseline disconnected'); return; }
      readGroups().then(g => fail(mine, refusal(g))).catch(() => fail(mine, refusal(null)));
    };
  }

  function stop() {
    session++;
    release();
    phase = 'idle';
    caller = null;
  }

  function change() {
    if (phase === 'connecting' || phase === 'listening') void play();
  }

  const who = $derived(caller ? callsign(String(caller), lookup) : null);
</script>

<div id="hoseline">
  <b>BrandMeister Hoseline</b>
  <table>
    <tbody>
      <tr>
        <th style="width:25%;"><a class="tooltip" href="#top">Talkgroup<span><b>BrandMeister talkgroup to listen to</b></span></a></th>
        <th style="width:25%;"><a class="tooltip" href="#top">Action<span><b>Listen in this browser (receive only)</b></span></a></th>
        <th><a class="tooltip" href="#top">Status<span><b>Relay status and current caller</b></span></a></th>
      </tr>
      <tr>
        <td>
          <select aria-label="Hoseline talkgroup" name="hoselineTalkgroup" bind:value={tg} onchange={change} disabled={!groups?.length}>
            {#each groups ?? [] as g (g)}<option value={String(g)}>TG {g}</option>{/each}
          </select>
        </td>
        <td>
          <input type="button" name="hoselinePlay" value="Play" onclick={play} disabled={!groups?.length} />
          <input type="button" name="hoselineStop" value="Stop" onclick={stop} disabled={phase === 'idle'} />
        </td>
        <td id="hoselineStatus"><span role="status">
          {#if groups === null}
            Loading…
          {:else if !groups.length}
            Talkgroups unavailable
          {:else if phase === 'connecting'}
            Connecting to TG {listening}…
          {:else if phase === 'listening'}
            Listening to TG {listening}{#if who}: {#if who.kind === 'text'}{who.text}{:else}<a href={who.href} target="_blank" rel="noopener noreferrer">{who.text}</a>{/if}{/if}
          {:else if phase === 'error'}
            {message}
          {:else}
            Stopped
          {/if}
        </span></td>
      </tr>
    </tbody>
  </table>
</div>
