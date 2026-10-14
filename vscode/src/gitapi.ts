import * as vscode from "vscode";

// The slice of extensions/git/src/api/git.d.ts this extension calls.
export interface GitRepository {
  readonly rootUri: vscode.Uri;
  readonly inputBox: { value: string };
  add(paths: string[]): Promise<void>;
  revert(paths: string[]): Promise<void>;
}

interface GitAPI {
  readonly state: "uninitialized" | "initialized";
  readonly onDidChangeState: vscode.Event<"uninitialized" | "initialized">;
  readonly onDidOpenRepository: vscode.Event<GitRepository>;
  getRepository(uri: vscode.Uri): GitRepository | null;
}

// gitRepository is Git's repository at exactly root: a worker's root is not here.
export async function gitRepository(
  root: string,
): Promise<GitRepository | undefined> {
  const ext = vscode.extensions.getExtension<{ getAPI(v: 1): GitAPI }>(
    "vscode.git",
  );
  if (!ext) return undefined;
  try {
    const api = (await ext.activate()).getAPI(1);
    if (api.state !== "initialized") await initialized(api);
    const repo =
      api.getRepository(vscode.Uri.file(root)) ?? (await opened(api, root));
    return repo && sameDir(repo.rootUri.fsPath, root) ? repo : undefined;
  } catch {
    return undefined;
  }
}

function sameDir(a: string, b: string): boolean {
  return a.replace(/\/+$/, "") === b.replace(/\/+$/, "");
}

// Git opens repositories after it reports itself initialized, so a miss waits
// briefly rather than settling the session on ade's own provider.
function opened(api: GitAPI, root: string): Promise<GitRepository | undefined> {
  return new Promise((done) => {
    const timer = setTimeout(() => finish(undefined), 2000);
    const sub = api.onDidOpenRepository((repo) => {
      if (sameDir(repo.rootUri.fsPath, root)) finish(repo);
    });
    function finish(repo: GitRepository | undefined) {
      clearTimeout(timer);
      sub.dispose();
      done(repo);
    }
  });
}

function initialized(api: GitAPI): Promise<void> {
  return new Promise((done) => {
    const timer = setTimeout(finish, 5000);
    const sub = api.onDidChangeState((s) => s === "initialized" && finish());
    function finish() {
      clearTimeout(timer);
      sub.dispose();
      done();
    }
  });
}
