import * as vscode from "vscode";
import type { BranchPR, Check, Listed } from "./client";
import {
  branchLine,
  checkIcon,
  checkLine,
  failing,
  prIcon,
  prLine,
  rankCheck,
} from "./issues";

// The rows under a checkout's Pull Requests node. The branch's own opens to
// its checks, which is what the web PR view was for.
export type PullNode =
  | { kind: "branch"; pr: BranchPR }
  | { kind: "listed"; pr: Listed }
  | { kind: "check"; check: Check; own: boolean };

// sortedChecks is the rows under a pull request, worst first. Own marks this
// branch's checks, the only ones Fix can act on.
export function sortedChecks(
  checks: Check[] | null | undefined,
  own: boolean,
): PullNode[] {
  return [...(checks ?? [])]
    .sort(rankCheck)
    .map((check) => ({ kind: "check", check, own }) as PullNode);
}

// The group row. Open when this branch has one: that row is the one with
// news on it, and a closed group hides which way its checks went.
export function pullsItem(count: number, branch: boolean): vscode.TreeItem {
  const item = new vscode.TreeItem(
    "Pull Requests",
    branch
      ? vscode.TreeItemCollapsibleState.Expanded
      : vscode.TreeItemCollapsibleState.Collapsed,
  );
  item.description = count > 0 ? String(count) : "none open";
  item.iconPath = new vscode.ThemeIcon("git-pull-request");
  item.contextValue = "ade.pulls";
  return item;
}

export function branchItem(pr: BranchPR): vscode.TreeItem {
  const checks = pr.checks ?? [];
  const item = new vscode.TreeItem(
    `#${pr.number} ${pr.title}`,
    checks.length > 0
      ? vscode.TreeItemCollapsibleState.Expanded
      : vscode.TreeItemCollapsibleState.None,
  );
  item.description = ["this branch", branchLine(pr)]
    .filter(Boolean)
    .join(" · ");
  item.iconPath = new vscode.ThemeIcon(
    failing(checks).length > 0 ? "error" : "git-pull-request",
    failing(checks).length > 0
      ? new vscode.ThemeColor("errorForeground")
      : undefined,
  );
  item.tooltip = new vscode.MarkdownString(pr.url);
  item.contextValue =
    failing(checks).length > 0 ? "ade.pr.branch.failing" : "ade.pr.branch";
  return item;
}

export function checkItem(check: Check, own: boolean): vscode.TreeItem {
  const item = new vscode.TreeItem(
    check.name,
    vscode.TreeItemCollapsibleState.None,
  );
  item.description = checkLine(check);
  item.iconPath = new vscode.ThemeIcon(
    checkIcon(check),
    check.bucket === "fail"
      ? new vscode.ThemeColor("errorForeground")
      : undefined,
  );
  item.tooltip = check.link || check.state;
  item.contextValue =
    check.bucket !== "fail"
      ? "ade.check"
      : own
        ? "ade.check.failing"
        : "ade.check.failing.listed";
  // The run's own page: what actually failed is in the log, not in the name.
  // The link is whatever the check's owner set, so only a web one is opened.
  if (check.link && /^https?:\/\//i.test(check.link)) {
    item.command = {
      command: "vscode.open",
      title: "Open the run",
      arguments: [vscode.Uri.parse(check.link)],
    };
  }
  return item;
}

export function listedItem(pr: Listed): vscode.TreeItem {
  // Collapsed, not None: the checks are read when the row is opened. A pull
  // request with nothing reporting opens to an empty row, which is the answer.
  const item = new vscode.TreeItem(
    `#${pr.number} ${pr.title}`,
    vscode.TreeItemCollapsibleState.Collapsed,
  );
  item.description = prLine(pr);
  item.iconPath = new vscode.ThemeIcon(prIcon(pr));
  item.tooltip = new vscode.MarkdownString(`**${pr.author}**\n\n${pr.url}`);
  item.contextValue = "ade.pr";
  // Opening it in ade rather than the browser: the diff and its threads are
  // what this extension has that github.com does not.
  item.command = {
    command: "ade.openPR",
    title: "Open in ade",
    arguments: [pr],
  };
  return item;
}
