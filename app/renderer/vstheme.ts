// The theme a panel sees: VS Code hands a webview `--vscode-*`
// variables, and nothing set them here.

// Ayu Dark Unbordered, as theme.css. The hexes are in two files because
// a panel cannot reach the window's stylesheet before its first paint.

// Anything not here falls back to the browser: add it rather than
// restyling the panel.
export const THEME = `
  --vscode-font-family: -apple-system, "Segoe UI", Ubuntu, "Droid Sans", sans-serif;
  --vscode-font-size: 13px;
  --vscode-editor-font-family: "Berkeley Mono", ui-monospace, "SF Mono", Menlo, Consolas, monospace;

  --vscode-foreground: #bfbdb6;
  --vscode-descriptionForeground: #5a6378;
  --vscode-icon-foreground: #5a6378;
  --vscode-errorForeground: #f26d78;
  --vscode-focusBorder: #e6b450;

  --vscode-editor-background: #0d1017;
  --vscode-editor-foreground: #bfbdb6;
  --vscode-sideBar-background: #0d1017;
  --vscode-editorWidget-background: #141821;
  --vscode-editorWidget-border: #1b1f29;
  --vscode-widget-border: #1b1f29;
  --vscode-menu-background: #141821;
  --vscode-panel-border: #1b1f29;

  --vscode-button-background: #e6b450;
  --vscode-button-foreground: #765b24;
  --vscode-button-border: transparent;
  --vscode-button-hoverBackground: #f0c368;
  --vscode-button-secondaryBackground: #47526633;
  --vscode-button-secondaryForeground: #bfbdb6;
  --vscode-button-secondaryHoverBackground: #47526640;

  --vscode-input-background: #10141c;
  --vscode-input-foreground: #bfbdb6;
  --vscode-input-border: #1b1f29;
  --vscode-inputValidation-errorBackground: #f26d7833;
  --vscode-inputValidation-errorBorder: #f26d78;
  --vscode-inputValidation-errorForeground: #bfbdb6;
  --vscode-inputValidation-infoBackground: #73b8ff33;
  --vscode-inputValidation-infoBorder: #73b8ff;

  --vscode-dropdown-background: #10141c;
  --vscode-dropdown-foreground: #bfbdb6;
  --vscode-dropdown-border: #1b1f29;
  --vscode-checkbox-background: #10141c;
  --vscode-checkbox-border: #2d333f;

  --vscode-badge-background: #47526633;
  --vscode-badge-foreground: #bfbdb6;

  --vscode-list-hoverBackground: #47526640;
  --vscode-list-activeSelectionBackground: #47526640;
  --vscode-list-activeSelectionForeground: #ffffff;
  --vscode-list-inactiveSelectionBackground: #47526633;
  --vscode-keybindingTable-headerBackground: #141821;
  --vscode-keybindingTable-rowsBackground: #10141c;

  --vscode-textLink-foreground: #73b8ff;
  --vscode-textLink-activeForeground: #8fc8ff;
  --vscode-textCodeBlock-background: #141821;
  --vscode-textBlockQuote-background: #141821;

  --vscode-terminal-ansiGreen: #aad94c;
  --vscode-terminal-ansiYellow: #e6b450;
  --vscode-charts-foreground: #bfbdb6;
  --vscode-charts-green: #aad94c;
  --vscode-charts-yellow: #e6b450;
  --vscode-charts-orange: #ff8f40;
  --vscode-charts-blue: #73b8ff;
  --vscode-charts-purple: #d2a6ff;

  --vscode-editorWarning-foreground: #e6b450;
  --vscode-statusBarItem-warningBackground: #e6b45033;
  --vscode-gitDecoration-addedResourceForeground: #70bf56;
  --vscode-gitDecoration-deletedResourceForeground: #f26d78;
`;

// EVERY is what the panels ask for, for the test that holds the two in
// step. One nothing sets is drawn in the browser's colour.
export function named(css: string): string[] {
  return [...new Set(css.match(/--vscode-[a-zA-Z-]+/g) ?? [])].sort();
}
