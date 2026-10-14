import * as vscode from "vscode";
import { Busy, indicator } from "./busy";
import { Fetch } from "./cache";

// An empty tree reads as "nothing here", not "not yet". A progress bar in the
// view's title beats a fake row, which would be selectable and count as a child.
export async function whileLoading<T>(
  viewId: string,
  work: () => Promise<T>,
): Promise<T> {
  return vscode.window.withProgress({ location: { viewId } }, work);
}

// ProgressLocation.Window is the status bar, which is on screen whatever the
// sidebar is doing. The view's own bar says which tree; this says anything.
const busy = new Busy((until) => {
  void vscode.window.withProgress(
    { location: vscode.ProgressLocation.Window, title: "ade: loading" },
    () => until,
  );
});

// A tree's contents, with both indicators around every read.
export function fetched<T>(
  viewId: string,
  load: () => Promise<T>,
  empty: T,
): Fetch<T> {
  return new Fetch(
    load,
    empty,
    indicator(busy, (work) => whileLoading(viewId, work)),
  );
}

// offstage is the status bar alone, for a read nothing on screen is waiting
// on: a picker has no view to put a progress bar in.
export function offstage<T>(work: () => Promise<T>): Promise<T> {
  return busy.around(work);
}

// held is a picker's contents, kept between openings the same way a tree's are.
export function held<T>(load: () => Promise<T>, empty: T): Fetch<T> {
  return new Fetch(load, empty, (work) => busy.around(work));
}

// loading is the same two indicators around work that is not a tree's read:
// a server-side refresh a view then displays is still the view loading.
export function loading<T>(viewId: string, work: () => Promise<T>): Promise<T> {
  return indicator(busy, (inner) => whileLoading(viewId, inner))(work);
}

// Views is every tree this extension still contributes. Everything else has no
// view to draw progress in and reads through held() or offstage().
export const VIEWS = {
  diff: "ade.diff",
} as const;
