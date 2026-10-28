import { downsample, wav } from "./wav";

// record opens the microphone and returns a stop that yields the take as WAV.
// Without a device it is the system default, which is silent on a clamshell.
export async function record(worklet: string, device?: string): Promise<() => Promise<Uint8Array>> {
  const stream = await navigator.mediaDevices.getUserMedia({
    audio: {
      channelCount: 1,
      echoCancellation: true,
      noiseSuppression: true,
      ...(device ? { deviceId: { exact: device } } : {}),
    },
  });
  const audio = new AudioContext();
  try {
    await audio.audioWorklet.addModule(worklet);
  } catch (err) {
    stream.getTracks().forEach((t) => t.stop());
    void audio.close();
    throw err;
  }
  const source = audio.createMediaStreamSource(stream);
  const tap = new AudioWorkletNode(audio, "ade-mic");
  const blocks: Float32Array[] = [];
  tap.port.onmessage = (event: MessageEvent<Float32Array>) => blocks.push(event.data);
  source.connect(tap);
  // A node nothing pulls from is not run; its output is silence.
  tap.connect(audio.destination);

  return async () => {
    source.disconnect();
    tap.port.onmessage = null;
    stream.getTracks().forEach((t) => t.stop());
    const rate = audio.sampleRate;
    await audio.close();
    const all = new Float32Array(blocks.reduce((n, b) => n + b.length, 0));
    let at = 0;
    for (const b of blocks) {
      all.set(b, at);
      at += b.length;
    }
    return wav(downsample(all, rate));
  };
}

// inputs is every microphone the page can see. Labels are blank until the
// first grant, so one is made up rather than drawn empty.
export async function inputs(): Promise<{ id: string; label: string }[]> {
  const all = await navigator.mediaDevices.enumerateDevices();
  return all
    .filter((d) => d.kind === "audioinput")
    .map((d, at) => ({ id: d.deviceId, label: d.label || `Microphone ${at + 1}` }));
}
