// The whole of what the shell gives the window: the keyboard, reaching
// ade, and the webview scheme. Everything else the window does.
export interface Bridge {
  onCommand(then: (command: string) => void): void;
  keys(): Promise<unknown[]>;

  // Everything the sidebar draws, in one round trip.
  world(dir?: string): Promise<unknown>;
  // One pull request in full, and one design's markdown.
  pr(repo: string, number: number): Promise<Record<string, unknown>>;
  // Brings the launcher up, which nothing else on the machine does.
  start(): Promise<{ ok?: boolean; error?: string }>;
  // The picked session's event stream. The panels are webviews and
  // expect their host to carry it; so does the window's own state.
  watch(session?: string): Promise<unknown>;
  onEvent(then: (name: string) => void): void;
  onStream(then: (open: boolean) => void): void;
  // The terminal: the socket is the shell's, the screen is the page's.
  term(session: string, pane: string): Promise<unknown>;
  termSend(data: string): void;
  onTerm(then: (data: string) => void): void;
  // A window of its own, holding one panel.
  float(panel: string, session: string): Promise<unknown>;
  // A link from a panel, opened in the browser.
  link(href: string): Promise<unknown>;

  // A verb from the table, and a read for a panel that needs one.
  act(
    at: "launcher" | "session",
    path: string,
    body: Record<string, unknown>,
    session?: string,
    method?: "POST" | "PUT" | "DELETE",
  ): Promise<{ error?: string } | undefined>;
  read(at: "launcher" | "session", path: string, session?: string): Promise<unknown>;
  // The raw one, for a panel's relay: status, ok, and the body as text.
  call(
    at: "launcher" | "session",
    path: string,
    method?: string,
    body?: string,
    session?: string,
  ): Promise<{ status: number; ok?: boolean; text?: string; error?: string }>;

  // A panel's page, held on the webview scheme; the answer is its url.
  panel(id: string, html: string): Promise<string>;
  // Where ade is, for a panel that fetches it directly.
  where(): Promise<{ base: string; token: string } | null>;
  // The checkout the window was opened on, when one was named.
  opened(): Promise<string>;
}

declare global {
  interface Window {
    ade?: Bridge;
  }
}
