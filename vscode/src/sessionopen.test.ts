import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { folderURI } from "./remote";

const src = readFileSync(join(__dirname, "extension.ts"), "utf8");

describe("opening a conversation that is on another machine", () => {
  // The path is that machine's. Opening it as a local folder is the error
  // "The path ... does not exist on this computer".
  it("goes through openThere rather than switchTo", () => {
    const body = src.slice(
      src.indexOf("async function openSession("),
      src.indexOf("// resume points a server's conversation"),
    );
    expect(body).toContain("openThere(machine");
    expect(body.indexOf("openThere(machine")).toBeLessThan(
      body.indexOf("switchTo(session.dir)"),
    );
  });

  it("addresses it as the machine's own folder", () => {
    expect(folderURI("ade@ade-owen", "/home/ade/code/x/ade")).toBe(
      "vscode-remote://ssh-remote+ade@ade-owen/home/ade/code/x/ade",
    );
  });
});
