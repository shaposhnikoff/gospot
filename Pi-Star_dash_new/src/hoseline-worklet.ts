// AudioWorklet of the Hoseline player: decoded 8 kHz samples arrive from the
// page and are played through a small jitter buffer. Bundled by Vite
// (?worker&url) into assets/, so it loads from 'self' under the admin CSP.
import { JitterBuffer, RATE } from './hoseline';

declare const sampleRate: number;
declare function registerProcessor(name: string, processor: unknown): void;
declare class AudioWorkletProcessor {
  readonly port: MessagePort;
}

class HoselineProcessor extends AudioWorkletProcessor {
  private readonly buffer = new JitterBuffer();

  constructor() {
    super();
    this.port.onmessage = (e: MessageEvent) => {
      const m = e.data as { type?: string; samples?: unknown };
      if (m?.type === 'audio' && m.samples instanceof Float32Array) this.buffer.push(m.samples);
      else if (m?.type === 'flush') this.buffer.flush();
      else if (m?.type === 'reset') this.buffer.reset();
    };
  }

  process(_inputs: Float32Array[][], outputs: Float32Array[][]): boolean {
    const channels = outputs[0];
    if (!channels?.length) return true;
    const step = RATE / sampleRate;
    const first = channels[0];
    for (let i = 0; i < first.length; i++) first[i] = this.buffer.next(step);
    for (let c = 1; c < channels.length; c++) channels[c].set(first);
    return true;
  }
}

registerProcessor('hoseline-audio', HoselineProcessor);
