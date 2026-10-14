// Incoming mirrors chat.Incoming: what one attachment looks like on the wire.
export interface Incoming {
  name: string;
  mediaType: string;
  data: string;
}

// The server's own caps, repeated here so a refusal is immediate rather than a
// round trip. chat.MaxFiles and chat.MaxFileBytes.
export const MAX_FILES = 10;
export const MAX_BYTES = 20 << 20;

// pasted is the images on a clipboard event. This is the whole reason the chat
// is not an iframe: a nested cross-origin frame never sees these.
export function pasted(items: DataTransferItemList | undefined): File[] {
  return [...(items || [])]
    .filter((item) => item.kind === "file" && item.type.startsWith("image/"))
    .map((item) => item.getAsFile())
    .filter((file): file is File => file !== null);
}

// refuse says why a file cannot go, or nothing when it can.
export function refuse(file: File, held: number): string {
  if (held >= MAX_FILES) return `${MAX_FILES} files at most`;
  if (file.size > MAX_BYTES) return `${file.name || "that file"} is over 20MB`;
  return "";
}

// read turns a file into what the API takes: base64 without the data: prefix,
// which the server adds back from mediaType.
export async function read(file: File): Promise<Incoming> {
  const buffer = await file.arrayBuffer();
  let binary = "";
  const bytes = new Uint8Array(buffer);
  // In chunks: spreading a 20MB array into apply blows the argument limit.
  for (let at = 0; at < bytes.length; at += 8192) {
    binary += String.fromCharCode(...bytes.subarray(at, at + 8192));
  }
  return {
    name: file.name || `pasted-${Date.now()}.png`,
    mediaType: file.type || "application/octet-stream",
    data: btoa(binary),
  };
}

// dropped is the file paths on a drag. The editor's tree gives these and no
// bytes, so only the extension host can read them.
export function dropped(list: string | undefined): string[] {
  return (list || "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.startsWith("file:"));
}

// mediaOf mirrors chat.mediaOf: the server keeps the file and works the type
// out from the name, so only the image prefix has to be right here.
export function mediaOf(name: string): string {
  const images: Record<string, string> = {
    png: "image/png",
    gif: "image/gif",
    webp: "image/webp",
    jpg: "image/jpeg",
    jpeg: "image/jpeg",
  };
  return (
    images[name.toLowerCase().split(".").pop() || ""] ||
    "application/octet-stream"
  );
}
