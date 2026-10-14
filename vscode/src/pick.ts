import * as vscode from "vscode";

type Item = vscode.QuickPickItem;
type Many = vscode.QuickPickOptions & { canPickMany: true };

let boxed: boolean | undefined;

// pick is showQuickPick in the omnibox: the big box in ade's editor, VS Code's
// own picker anywhere else. Strings or items, one or many, as there.
export function pick<T extends Item>(
  items: T[] | Thenable<T[]>,
  options: Many,
): Promise<T[] | undefined>;
export function pick<T extends Item>(
  items: T[] | Thenable<T[]>,
  options?: vscode.QuickPickOptions,
): Promise<T | undefined>;
export function pick(
  items: string[] | Thenable<string[]>,
  options: Many,
): Promise<string[] | undefined>;
export function pick(
  items: string[] | Thenable<string[]>,
  options?: vscode.QuickPickOptions,
): Promise<string | undefined>;
export async function pick(
  items: (string | Item)[] | Thenable<(string | Item)[]>,
  options: vscode.QuickPickOptions = {},
): Promise<unknown> {
  await large();
  return vscode.window.showQuickPick(
    items as Item[] | Thenable<Item[]>,
    options,
  );
}

// input is showInputBox there.
export async function input(
  options?: vscode.InputBoxOptions,
  token?: vscode.CancellationToken,
): Promise<string | undefined> {
  await large();
  return vscode.window.showInputBox(options, token);
}

// quickPick and inputBox are the widgets a list with buttons or live rows
// needs. Each show asks for the big box again.
export function quickPick<T extends Item>(): vscode.QuickPick<T> {
  return shown(vscode.window.createQuickPick<T>());
}

export function inputBox(): vscode.InputBox {
  return shown(vscode.window.createInputBox());
}

export function shown<B extends { show(): void }>(box: B): B {
  const show = box.show.bind(box);
  box.show = () => void large().then(show);
  return box;
}

// large asks the editor for the big box; the editor clears it when the quick
// input that follows hides. Stock VS Code has nothing to ask.
async function large(): Promise<void> {
  if (!(await inBox())) return;
  try {
    await vscode.commands.executeCommand("ade.omnibox.large");
  } catch {
    // The small widget is the fallback, not a refusal.
  }
}

async function inBox(): Promise<boolean> {
  if (boxed === undefined) {
    try {
      const all = await vscode.commands.getCommands(true);
      boxed = all.includes("ade.omnibox.large");
    } catch {
      boxed = false;
    }
  }
  return boxed;
}
