/*---------------------------------------------------------------------------------------------
 *  Copyright (c) Microsoft Corporation. All rights reserved.
 *  Licensed under the MIT License. See License.txt in the project root for license information.
 *--------------------------------------------------------------------------------------------*/

// Trimmed to what ade's ade-ssh resolver uses. The two registrations are
// optional here because the editor only puts them on the api when it grants
// the proposal, which is a line in editor/product.json.
declare module "vscode" {
  export class ResolvedAuthority {
    readonly host: string;
    readonly port: number;
    readonly connectionToken: string | undefined;

    constructor(host: string, port: number, connectionToken?: string);
  }

  export class RemoteAuthorityResolverError extends Error {
    static NotAvailable(
      message?: string,
      handled?: boolean,
    ): RemoteAuthorityResolverError;
    static TemporarilyNotAvailable(
      message?: string,
    ): RemoteAuthorityResolverError;
  }

  export interface RemoteAuthorityResolver {
    resolve(
      authority: string,
    ): ResolvedAuthority | Thenable<ResolvedAuthority>;
  }

  export interface ResourceLabelFormatting {
    label: string;
    separator: "/" | "\\" | "";
    tildify?: boolean;
    normalizeDriveLetter?: boolean;
    workspaceSuffix?: string;
    workspaceTooltip?: string;
    authorityPrefix?: string;
    stripPathStartingSeparator?: boolean;
  }

  export interface ResourceLabelFormatter {
    scheme: string;
    authority?: string;
    formatting: ResourceLabelFormatting;
  }

  export namespace workspace {
    export const registerRemoteAuthorityResolver:
      | ((
          authorityPrefix: string,
          resolver: RemoteAuthorityResolver,
        ) => Disposable)
      | undefined;

    export const registerResourceLabelFormatter:
      | ((formatter: ResourceLabelFormatter) => Disposable)
      | undefined;
  }
}
