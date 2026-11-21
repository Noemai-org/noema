// A19/A97: 16 kHz, signed 16-bit little-endian, one channel. No transport.
export function encodePcm16Mono(samples: Float32Array): Uint8Array {
  const output = new Uint8Array(samples.length * 2);
  const view = new DataView(output.buffer);
  for (let i = 0; i < samples.length; i++) {
    const value = samples[i]!;
    if (!Number.isFinite(value)) throw new Error("Invalid audio sample.");
    const clipped = Math.max(-1, Math.min(1, value));
    view.setInt16(i * 2, Math.round(clipped * (clipped < 0 ? 32768 : 32767)), true);
  }
  return output;
}

// A191: the model service rejects a PCM frame over 32,000 bytes. The bound is
// even, so splitting the encoded bytes never breaks a 16-bit sample.
export const MAX_PCM_FRAME_BYTES = 32_000;
export function encodePcm16Frames(samples: Float32Array): Uint8Array[] {
  const encoded = encodePcm16Mono(samples);
  const frames: Uint8Array[] = [];
  for (let offset = 0; offset < encoded.length; offset += MAX_PCM_FRAME_BYTES) {
    frames.push(encoded.slice(offset, offset + MAX_PCM_FRAME_BYTES));
  }
  return frames;
}

// The caller supplies the user-controlled lifetime and the sink. Never persist audio.
export async function startPcmCapture(
  onPcm: (bytes: Uint8Array) => void,
  signal: AbortSignal,
  onFailure: (() => void) | undefined,
  maxRecordingMs: number,
): Promise<() => Promise<void>> {
  if (signal.aborted || !Number.isSafeInteger(maxRecordingMs) || maxRecordingMs <= 0 || maxRecordingMs > 2_147_483_647) throw new Error("Voice is unavailable.");
  const startedAt = performance.now();
  const reportFailure = onFailure ?? (() => {});
  const stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1 }, video: false });
  let context: AudioContext | undefined;
  let source: MediaStreamAudioSourceNode | undefined;
  let processor: ScriptProcessorNode | undefined;
  let muted: GainNode | undefined;
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const tracks = stream.getAudioTracks();
  const lostTrack = () => { void stop().then(reportFailure, reportFailure); };
  const stop = async () => {
    if (stopped) return;
    stopped = true;
    if (timer !== undefined) clearTimeout(timer);
    signal.removeEventListener("abort", abort);
    if (processor) processor.onaudioprocess = null;
    source?.disconnect();
    processor?.disconnect();
    muted?.disconnect();
    for (const track of tracks) track.removeEventListener("ended", lostTrack);
    for (const track of stream.getTracks()) track.stop();
    if (context && context.state !== "closed") await context.close();
  };
  const abort = () => { void stop().catch(reportFailure); };
  try {
    if (signal.aborted) { await stop(); return stop; }
    const remaining = maxRecordingMs - (performance.now() - startedAt);
    if (remaining <= 0) { await stop(); throw new Error("Voice is unavailable."); }
    timer = setTimeout(() => { void stop().then(reportFailure, reportFailure); }, remaining);
    context = new AudioContext({ sampleRate: 16000 });
    if (context.sampleRate !== 16000) throw new Error("Unsupported audio rate.");
    if (tracks.length !== 1 || tracks[0]?.readyState !== "live") throw new Error("Voice is unavailable.");
    for (const track of tracks) track.addEventListener("ended", lostTrack, { once: true });
    source = context.createMediaStreamSource(stream);
    processor = context.createScriptProcessor(0, 1, 1);
    muted = context.createGain();
    muted.gain.value = 0;
    processor.onaudioprocess = (event) => {
      if (stopped || signal.aborted) return;
      try {
        if (event.inputBuffer.numberOfChannels !== 1) throw new Error("Invalid audio channels.");
        for (const frame of encodePcm16Frames(event.inputBuffer.getChannelData(0))) {
          if (stopped || signal.aborted) break;
          onPcm(frame);
        }
      } catch {
        void stop().then(reportFailure, reportFailure);
      }
    };
    source.connect(processor);
    processor.connect(muted);
    muted.connect(context.destination);
    signal.addEventListener("abort", abort, { once: true });
    if (signal.aborted) { await stop(); return stop; }
    await context.resume();
    return stop;
  } catch {
    await stop();
    throw new Error("Voice is unavailable.");
  }
}
