// What the tests need of the vscode module, which only an extension host has.
// vitest.config.ts aliases "vscode" here; nothing in the extension imports it.
export enum TreeItemCollapsibleState {
  None = 0,
  Collapsed = 1,
  Expanded = 2,
}

export enum QuickPickItemKind {
  Separator = -1,
  Default = 0,
}

export enum ViewColumn {
  Active = -1,
  Beside = -2,
}

export enum ProgressLocation {
  Window = 10,
}

export class ThemeColor {
  constructor(public id: string) {}
}

export class ThemeIcon {
  constructor(
    public id: string,
    public color?: ThemeColor,
  ) {}
}

export class MarkdownString {
  constructor(public value = "") {}
}

export class Uri {
  private constructor(
    public scheme: string,
    public fsPath: string,
  ) {}

  static file(path: string): Uri {
    return new Uri("file", path);
  }

  static parse(value: string): Uri {
    return new Uri(value.split(":")[0] ?? "", value);
  }

  static joinPath(base: Uri, ...bits: string[]): Uri {
    return new Uri(base.scheme, [base.fsPath, ...bits].join("/"));
  }

  toString(): string {
    return this.scheme === "file" ? `file://${this.fsPath}` : this.fsPath;
  }
}

export class TreeItem {
  description?: string;
  tooltip?: string | MarkdownString;
  iconPath?: ThemeIcon;
  contextValue?: string;
  resourceUri?: Uri;
  command?: { command: string; title: string; arguments?: unknown[] };

  constructor(
    public label: string,
    public collapsibleState = TreeItemCollapsibleState.None,
  ) {}
}

export class EventEmitter<T> {
  private listeners: ((e: T) => void)[] = [];

  event = (listener: (e: T) => void): { dispose(): void } => {
    this.listeners.push(listener);
    return {
      dispose: () => {
        this.listeners = this.listeners.filter((l) => l !== listener);
      },
    };
  };

  fire(e: T): void {
    for (const listener of this.listeners) listener(e);
  }

  dispose(): void {
    this.listeners = [];
  }
}

export const workspace = {
  openTextDocument: async (..._args: unknown[]): Promise<unknown> => ({}),
  getConfiguration: (_section?: string) => ({
    get: <T>(_key: string, fallback?: T): T | undefined => fallback,
  }),
};

export const commands = {
  executeCommand: async (_id: string, ..._args: unknown[]): Promise<unknown> =>
    undefined,
};

export const window = {
  state: { focused: true },
  createWebviewPanel: (..._args: unknown[]) => ({
    webview: {
      options: {},
      html: "",
      cspSource: "vscode-resource:",
      asWebviewUri: (uri: unknown) => uri,
      onDidReceiveMessage: () => ({ dispose() {} }),
      postMessage: async () => true,
    },
    onDidDispose: () => ({ dispose() {} }),
    reveal: () => {},
    dispose: () => {},
    viewColumn: 1,
  }),
  onDidChangeWindowState: (_l: (e: { focused: boolean }) => void) => ({
    dispose: () => {},
  }),
  showWarningMessage: async (..._args: unknown[]): Promise<string | undefined> =>
    undefined,
  tabGroups: {
    all: [] as { tabs: { input?: unknown }[] }[],
    close: async (_tabs: unknown): Promise<boolean> => true,
  },
  showTextDocument: async (..._args: unknown[]): Promise<unknown> => undefined,
  setStatusBarMessage: (_text: string, _hide?: number) => ({ dispose() {} }),
  withProgress: <R>(_options: unknown, work: () => Promise<R>): Promise<R> =>
    work(),
};

export enum InputBoxValidationSeverity {
  Info = 1,
  Warning = 2,
  Error = 3,
}
