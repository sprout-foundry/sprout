/**
 * Editor / semantic language service API types.
 */

export interface DiagnosticEntry {
  from: number;
  to: number;
  severity: 'error' | 'warning' | 'info';
  message: string;
  source: string;
}

export interface DiagnosticsResponse {
  message: string;
  path: string;
  diagnostics: DiagnosticEntry[];
  version: string;
}

export interface SemanticCapabilities {
  diagnostics: boolean;
  definition: boolean;
}

export interface SemanticDiagnosticsResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities;
  diagnostics: DiagnosticEntry[];
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticDefinitionResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities;
  definition?: { path: string; line: number; column: number } | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticHoverResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { hover: boolean };
  hover?: { contents: string } | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticRenameResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { hover: boolean; rename: boolean };
  rename?: { locations: Array<{ filePath: string; from: number; to: number }> } | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticReferencesResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { hover: boolean; rename: boolean; references: boolean };
  references?: {
    locations: Array<{ filePath: string; line: number; startCol: number; endCol: number; lineText: string }>;
    symbolName: string;
  } | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticCodeActionsResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { hover: boolean; rename: boolean; references: boolean; code_actions: boolean };
  code_actions?: Array<{
    title: string;
    kind: string;
    edits: Array<{ filePath: string; from: number; to: number; newText: string }>;
  }> | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticInlayHintsResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { inlay_hints: boolean };
  inlay_hints?: Array<{ from: number; to: number; label: string; kind: 'type' | 'parameter' | 'none' }> | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface SemanticSignatureHelpResponse {
  message: string;
  path: string;
  language_id: string;
  method: string;
  capabilities: SemanticCapabilities & { hover: boolean; signature_help: boolean };
  signature_help?: {
    signatures: Array<{
      label: string;
      documentation?: string;
      parameters: Array<{
        label: string;
        documentation?: string;
      }>;
    }>;
    activeSignature: number;
    activeParameter: number;
  } | null;
  duration_ms?: number;
  error?: string;
  version: string;
}

export interface WorkspaceSymbolEntry {
  name: string;
  kind: string;
  line?: number;
}

export interface WorkspaceSymbolFile {
  file: string;
  symbols: WorkspaceSymbolEntry[];
}

export interface WorkspaceSymbolsResponse {
  message: string;
  files: WorkspaceSymbolFile[];
  total: number;
}
