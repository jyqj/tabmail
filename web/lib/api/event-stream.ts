export type EventStreamFrame = {
  type: string;
  // Undefined means no data field; an empty data field still dispatches.
  data?: string;
  // Undefined preserves the cursor; an empty id explicitly clears it.
  id?: string;
};

// Bound one incomplete frame, not a network chunk containing many frames.
// Count decoded UTF-16 code units, with each line ending counted once.
export const MAX_EVENT_FRAME_LENGTH = 1024 * 1024;

// Feed TextDecoder's streaming output. A new parser belongs to each connection:
// EOF discards its unfinished frame, while the caller retains completed IDs.
export function createEventStreamParser(onFrame: (frame: EventStreamFrame) => void) {
  let line = "";
  let skipLF = false;
  let frameLength = 0;
  let type = "";
  let id: string | undefined;
  let data: string[] = [];

  function count(length: number) {
    frameLength += length;
    if (frameLength > MAX_EVENT_FRAME_LENGTH)
      throw new Error("Oversized event stream frame");
  }

  function finishLine() {
    const current = line;
    line = "";
    if (!current) {
      const frame: EventStreamFrame = { type: type || "message" };
      if (id !== undefined) frame.id = id;
      if (data.length) frame.data = data.join("\n");
      frameLength = 0;
      type = "";
      id = undefined;
      data = [];
      if (frame.id !== undefined || frame.data !== undefined) onFrame(frame);
      return;
    }
    if (current.startsWith(":")) return;
    const colon = current.indexOf(":");
    const field = colon < 0 ? current : current.slice(0, colon);
    let value = colon < 0 ? "" : current.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    if (field === "data") data.push(value);
    else if (field === "event") type = value;
    else if (field === "id" && !value.includes("\0")) id = value;
    // Unknown fields (including retry) cannot change our bounded backoff.
  }

  return {
    push(chunk: string) {
      let start = 0;
      for (let index = 0; index < chunk.length; index++) {
        const char = chunk[index];
        if (skipLF) {
          skipLF = false;
          if (char === "\n") { start = index + 1; continue; }
        }
        if (char !== "\r" && char !== "\n") continue;
        count(index - start + 1);
        line += chunk.slice(start, index);
        skipLF = char === "\r";
        finishLine();
        start = index + 1;
      }
      count(chunk.length - start);
      line += chunk.slice(start);
    },
  };
}
