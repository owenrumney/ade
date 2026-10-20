import { describe, expect, it, vi } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  AUTHORITY,
  dispatch,
  Failed,
  hostOf,
  label,
  NO_LAUNCHER,
  noGh,
  noServer,
  published,
  releaseVersion,
  resolve,
  serverURL,
} from "./resolver";
import type { Deps } from "./resolver";

const src = readFileSync(join(__dirname, "extension.ts"), "utf8");
const template =
  "https://github.com/owenrumney/ade/releases/download/1.135.06783/ade-reh-${os}-${arch}-1.135.06783.tar.gz";

const server = { base: "http://127.0.0.1:24590", token: "t", root: "/code/ade" };

function deps(over: Partial<Deps> = {}): Deps {
  return {
    launcher: async () => server,
    editorServer: async () => ({ port: 41000, token: "secret", commit: "abc" }),
    unvalidated: async () => false,
    log: () => {},
    ...over,
  };
}

describe("the authority ade resolves", () => {
  it("is ade's own, and the host is the destination it bookmarked", () => {
    expect(AUTHORITY).toBe("ade-ssh");
    expect(hostOf("ade-ssh+pi@pi5.local")).toBe("pi@pi5.local");
    expect(hostOf("ade-ssh+owen@bigboy.local")).toBe("owen@bigboy.local");
    // The resolver is handed the whole authority, but a bare host is a host.
    expect(hostOf("pi@pi5.local")).toBe("pi@pi5.local");
  });

  it("reads as the machine, not as the authority", () => {
    expect(label("pi@pi5.local")).toBe("ade: pi@pi5.local");
  });
});

describe("resolving a window on another machine", () => {
  it("is the forwarded port on this machine and the host's token", async () => {
    const got = await resolve("ade-ssh+pi@pi5.local", deps());
    expect(got).toEqual({
      host: "pi@pi5.local",
      port: 41000,
      token: "secret",
      commit: "abc",
    });
  });

  // Starting one from inside a resolve would put a second ade on the machine,
  // for the one case where a local window is right there.
  it("is a hard error with no launcher, and never starts one", async () => {
    const err = await resolve("ade-ssh+pi", deps({ launcher: async () => undefined })).catch(
      (e: unknown) => e,
    );
    expect(err).toBeInstanceOf(Failed);
    expect((err as Failed).retry).toBe(false);
    expect((err as Failed).message).toBe(NO_LAUNCHER);
  });

  // A laptop that slept, a dropped tailnet, a server past its idle shutdown:
  // the workbench retries these on its own backoff.
  it("is retryable for anything the launcher could not reach", async () => {
    const err = await resolve(
      "ade-ssh+pi",
      deps({
        editorServer: async () => {
          throw new Error("no route to host");
        },
      }),
    ).catch((e: unknown) => e);
    expect((err as Failed).retry).toBe(true);
    expect((err as Failed).message).toContain("no route to host");
  });

  it("asks the launcher every time, so the resolve is the reconnect", async () => {
    const editorServer = vi.fn(async () => ({
      port: 1,
      token: "x",
      commit: "c",
    }));
    const d = deps({ editorServer });
    await resolve("ade-ssh+pi", d);
    await resolve("ade-ssh+pi", d);
    expect(editorServer).toHaveBeenCalledTimes(2);
  });

  it("passes the one-shot unvalidated flag through to the launcher", async () => {
    const editorServer = vi.fn(async () => ({
      port: 1,
      token: "x",
      commit: "c",
    }));
    await resolve(
      "ade-ssh+pi",
      deps({ editorServer, unvalidated: async () => true }),
    );
    expect(editorServer).toHaveBeenCalledWith(server, "pi", true);
  });
});

describe("the preflight for a server nobody built", () => {
  it("resolves the template product.json carries", () => {
    expect(serverURL(template)).toBe(
      "https://github.com/owenrumney/ade/releases/download/1.135.06783/ade-reh-linux-x64-1.135.06783.tar.gz",
    );
    expect(releaseVersion(template)).toBe("1.135.06783");
  });

  it("is a 404 and nothing else", async () => {
    expect(await published("https://x/y", async () => 200)).toBe(true);
    expect(await published("https://x/y", async () => 302)).toBe(true);
    expect(await published("https://x/y", async () => 404)).toBe(false);
  });

  // A network that cannot answer is not a missing server, and refusing the
  // window would be the wrong diagnosis.
  it("believes the server is there when the HEAD itself fails", async () => {
    expect(
      await published("https://x/y", () => Promise.reject(new Error("offline"))),
    ).toBe(true);
  });

  it("offers the remedy, naming the run and the fallback command", () => {
    expect(dispatch("1.135.06783")).toEqual([
      "workflow",
      "run",
      "editor-reh.yml",
      "-f",
      "release_version=1.135.06783",
    ]);
    expect(noGh("1.135.06783")).toContain("gh workflow run editor-reh.yml");
    expect(noServer("1.135.06783")).toContain("1.135.06783");
  });
});

// The resolver has to be registered before the window it is for finishes
// opening, and onStartupFinished is too late for that.
describe("what the extension does with the resolver", () => {
  it("activates on the authority and registers it first", () => {
    const pkg = JSON.parse(
      readFileSync(join(__dirname, "..", "package.json"), "utf8"),
    ) as { activationEvents: string[]; extensionKind: string[] };
    expect(pkg.activationEvents).toContain("onResolveRemoteAuthority:ade-ssh");
    // A ui extension stays on this machine in a remote window, which is where
    // the launcher it talks to is.
    expect(pkg.extensionKind).toEqual(["ui"]);
    expect(src).toContain("registerRemoteAuthorityResolver(AUTHORITY");
  });

  it("maps a hard failure and a retryable one to the workbench's two", () => {
    const body = src.slice(src.indexOf("async function resolveHost("));
    expect(body).toContain("RemoteAuthorityResolverError.NotAvailable");
    expect(body).toContain(
      "RemoteAuthorityResolverError.TemporarilyNotAvailable",
    );
  });

  // A first install is 120 MiB of curl and an untar on a pi. resolve() has no
  // progress channel of its own, so the extension raises one.
  it("says what it is doing for the minutes an install takes", () => {
    expect(src).toContain("Remote - ade");
    const body = src.slice(src.indexOf("async function resolveHost("));
    expect(body).toContain("withProgress");
    expect(body).toContain("120 MiB");
  });
});
