import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// A webview's html is readable from any window's devtools, so the token must
// not be in it. The design says so and the first build did it anyway.
const chat = readFileSync(join(__dirname, "chat.ts"), "utf8");

// The body is shared with the app, which hosts the same page, so the
// markup is read where it lives rather than where it is wrapped.
const shared = readFileSync(join(__dirname, "chat", "body.ts"), "utf8");

// The markup the page is built from, without the code around it.
const markup = chat.slice(
  chat.indexOf("<!doctype html>"),
  chat.lastIndexOf("</html>"),
);

describe("the chat webview's html", () => {
  it("does not interpolate the token into the markup", () => {
    expect(markup).not.toContain("server.token");
    expect(markup).not.toContain("data-token");
  });

  it("hands the token over by postMessage instead", () => {
    expect(chat).toContain("postMessage");
    expect(chat).toContain('kind: "token"');
  });

  // Without the base the page cannot find ade at all, and the base is not a
  // credential: it is a loopback port anything on this machine can see.
  // Not server.base: a webview renders locally even when the editor is on
  // another machine, so the page is given a forwarded address.
  it("carries the address the page can reach, which is not a secret", () => {
    expect(shared).toContain('data-base="${base}"');
    expect(chat).toContain("reach ?? server.base");
  });

  it("keeps a nonce on the script and no inline handlers", () => {
    expect(markup).toContain("nonce-");
    expect(markup).not.toContain("onclick=");
  });

  // Loopback, plus wherever a forwarded address landed: that one is computed,
  // so the policy has to name it rather than assume a port on this machine.
  it("allows loopback and the address it was given, and nothing else", () => {
    expect(markup).toContain(
      "connect-src ${origin} http://127.0.0.1:* http://localhost:*",
    );
    expect(chat).toContain("const origin = new URL(base).origin");
    expect(markup).toContain("default-src 'none'");
  });
});

// A pane that renders nothing is the one state that cannot be diagnosed from
// the outside, and on a remote window asExternalUri may never answer.
describe("the chat pane never goes blank", () => {
  it("writes the page before it waits for a forwarded address", () => {
    expect(chat).toContain("private draw(webview: vscode.Webview): void");
    expect(chat).toMatch(/this\.write\([\s\S]*?\)\;[\s\S]*?void this\.where\(\)/);
  });

  it("says what failed when the page cannot be built", () => {
    expect(chat).toContain("webview.html = broken(String(err))");
    expect(chat).toContain("ade could not draw the conversation.");
  });
});

describe("the chat script", () => {
  const main = readFileSync(join(__dirname, "chat", "main.ts"), "utf8");

  it("asks for the token rather than waiting to be told", () => {
    // Posting first would race the script's own listener registration.
    expect(main).toContain('postMessage({ kind: "ready" })');
  });

  it("does not read the token off the element any more", () => {
    expect(main).not.toContain("dataset.token");
  });

  // The page makes no request of its own: the editor does, so a webview on
  // one machine can be about an ade on another.
  it("asks the editor rather than fetching", () => {
    expect(main).not.toContain("fetch(url(");
    expect(main).not.toContain("new EventSource");
    expect(main).toContain('host.postMessage({ kind: "api"');
  });
});

describe("the attachment the editor puts on the next message", () => {
  const main = readFileSync(join(__dirname, "chat", "main.ts"), "utf8");

  // Without this, Cmd+Alt+A posts the selection and the pane never shows it —
  // it pops into whichever browser tab polls state next.
  it("is read off the state the pane already fetches", () => {
    expect(main).toContain("takeAttachment(state.attachment)");
  });

  it("goes out with the message rather than being decoration", () => {
    expect(main).toContain("queue: running, context");
  });

  it("is cleared by number, so a newer selection survives", () => {
    expect(main).toContain("/api/attach?seq=");
  });

  // The delete and the next state can cross, so the same one arrives twice.
  it("is not taken twice", () => {
    expect(main).toContain("taken.has(waiting.seq)");
  });
});

describe("the chat stream when ade goes away and comes back", () => {
  const main = readFileSync(join(__dirname, "chat", "main.ts"), "utf8");
  const chatSrc = readFileSync(join(__dirname, "chat.ts"), "utf8");

  // This used to skip rebuilding when the token had not changed, to save a
  // socket. It is why a pane that lost the stream stayed silent: the
  // extension re-sends the same token on reconnect, and that was ignored.
  it("rebuilds on any token, changed or not", () => {
    expect(main).not.toContain("said.token === token");
  });

  it("clears the notice when the stream opens again", () => {
    expect(main).toContain('said?.kind === "stream"');
    expect(main).toContain("lost the connection to ade");
  });

  // Rewriting the html throws away a typed draft and any held images.
  it("hands a new token over instead of reloading the page", () => {
    expect(chatSrc).toContain("was?.base === server?.base");
    expect(chatSrc).toContain("this.hand()");
  });
});

describe("the chat pane recovering", () => {
  const main = readFileSync(join(__dirname, "chat", "main.ts"), "utf8");
  const host = readFileSync(join(__dirname, "chat.ts"), "utf8");

  // The stream lives in the extension host now, which is the only process
  // sure of reaching ade. Its reconnect is the one that matters.
  it("reconnects from the editor rather than the page", () => {
    const listen = host.slice(host.indexOf("private listen("));
    expect(listen).toContain("streamRetry");
    expect(listen).toContain("stop.signal.aborted");
  });

  // A watcher left over from an earlier server would speak for the wrong one.
  it("lets only the current watcher speak", () => {
    const listen = host.slice(host.indexOf("private listen("));
    expect(listen).toContain("this.watching?.abort()");
    expect(listen).toContain("this.server !== server");
  });

  // The events carry no id, so a reconnected stream cannot resume: whatever
  // happened while it was down arrived nowhere.
  it("refetches when the stream opens, not just when an event arrives", () => {
    const opened = main.slice(main.indexOf('said?.kind === "stream"'));
    expect(opened.slice(0, opened.indexOf("return;"))).toContain("start()");
    const named = main.slice(main.indexOf('said?.kind === "event"'));
    expect(named.slice(0, named.indexOf("return;"))).toContain("refresh()");
  });

  // The send worked; only the redraw was missing, which reads as a failure.
  it("redraws after a send rather than waiting for the stream", () => {
    const submit = main.slice(main.indexOf("async function submit"));
    expect(submit.slice(0, submit.indexOf("} catch"))).toContain(
      "await refresh()",
    );
  });

  // The poll redraws every few seconds, and a failing check's output that
  // shut while you were reading it was the first thing this got wrong.
  it("keeps a check's output open across a redraw", () => {
    const note = main.slice(main.indexOf("if (msg.output)"));
    const built = note.slice(0, note.indexOf("return el;"));
    expect(built).toContain("unfolded.has(msg.id)");
    expect(built).toContain("unfolded.add(msg.id)");
  });

  // Stop is the turn, not the session: it posts the cancel and shows only
  // while there is a turn to cancel.
  it("offers Stop while a turn runs, and cancels only the turn", () => {
    const cancel = main.slice(main.indexOf("async function cancel"));
    expect(cancel.slice(0, cancel.indexOf("} catch"))).toContain(
      '"/api/cancel"',
    );
    expect(main).toContain("stop.hidden = !running;");
  });
});

// The conversation has a container of its own, so it can be dragged to the
// secondary sidebar. It was a section among nine trees once and got 230px.
describe("where the chat lives", () => {
  const src = readFileSync(join(__dirname, "chat.ts"), "utf8");
  const manifest = JSON.parse(
    readFileSync(join(__dirname, "..", "package.json"), "utf8"),
  ) as {
    contributes: {
      viewsContainers: { activitybar: { id: string }[] };
      views: Record<string, { id: string; type?: string }[]>;
    };
  };

  it("is a webview in a container of its own", () => {
    expect(
      manifest.contributes.viewsContainers.activitybar.map((c) => c.id),
    ).toContain("ade-chat");
    expect(manifest.contributes.views["ade-chat"]).toEqual([
      { id: "ade.chat", name: "Chat", type: "webview" },
    ]);
  });

  // Among the nine trees it got a strip. A container is dragged as a unit.
  it("is not among the ADE trees", () => {
    expect(manifest.contributes.views.ade.map((v) => v.id)).not.toContain(
      "ade.chat",
    );
  });

  it("still has the editor panel, for when the view is not open", () => {
    expect(src).toContain("createWebviewPanel");
    expect(src).toContain("vscode.ViewColumn.Beside");
    expect(src).not.toContain("vscode.ViewColumn.Active");
  });

  // Beside alone is not enough: an open goes to the active group.
  it("locks the panel's group so nothing else opens into it", () => {
    expect(src).toContain("workbench.action.lockEditorGroup");
    expect(src).toMatch(/reveal\([\s\S]*lockEditorGroup/);
  });

  it("hands the token to every home it has", () => {
    expect(src).toMatch(/for \(const webview of this\.webviews\(\)\)/);
  });
});

// goldmark anchors a url in prose and leaves one in a fence, in inline code or
// in a command's output as plain text.
describe("a url the agent printed inside a fence", () => {
  const main = readFileSync(join(__dirname, "chat", "main.ts"), "utf8");
  const css = readFileSync(join(__dirname, "..", "media", "chat.css"), "utf8");

  it("is linked in the rendered markdown, before the copy buttons go on", () => {
    expect(main).toContain("linkify(body);");
    expect(main.indexOf("linkify(body);")).toBeLessThan(
      main.indexOf("addCopyButtons(body);"),
    );
  });

  it("is linked in a folded note's output too", () => {
    const at = main.indexOf("out.textContent = msg.output;");
    expect(main.slice(at, at + 80)).toContain("linkify(out);");
  });

  it("leaves an anchor the renderer already made alone", () => {
    expect(main).toContain('text.parentElement?.closest("a")');
  });

  it("looks like a link in output that is not a bubble", () => {
    expect(css).toContain(".note-row a {");
  });
});

// The webview host has its own anchor handler on document.body. A click that
// reaches it is opened twice: once by ade, once by the host.
describe("a link in the conversation", () => {
  const main = readFileSync(join(__dirname, "chat", "main.ts"), "utf8");
  const handler = main.slice(
    main.indexOf("const openLink"),
    main.indexOf('list.addEventListener("auxclick"'),
  );

  it("stops the click before the host's own handler sees it", () => {
    expect(handler).toContain("event.preventDefault()");
    expect(handler).toContain("event.stopPropagation()");
  });

  // Ctrl- and cmd-click arrive as click; middle click arrives as auxclick, and
  // the host opens that one without asking.
  it("takes the middle click too", () => {
    expect(main).toContain('list.addEventListener("auxclick", openLink)');
  });

  it("still gates on the scheme, so a command: uri is never followed", () => {
    expect(handler).toContain("openable(href)");
  });
});
