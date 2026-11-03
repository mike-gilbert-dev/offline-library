// Mirrors the JSON returned by the Go server (server/library.go).

export type PackStatus = 'ok' | 'unsupported' | 'incomplete' | 'invalid';

export interface FileEntry {
  path: string;
  sizeBytes: number;
  sha256?: string;
}

export interface PackManifest {
  schemaVersion: number;
  packId: string;
  title: string;
  description?: string;
  category: string;
  format: string;
  version: string;
  languages: string[] | null;
  contentDate: string;
  entrypoint: string;
  files: FileEntry[] | null;
  source: { name: string; url: string; license: string; attribution: string };
  viewer?: { style?: { light?: string; dark?: string } };
}

export interface Pack {
  manifestPath: string;
  status: PackStatus;
  problems: string[];
  manifest?: PackManifest;
  totalBytes: number;
}

export interface Library {
  root: string;
  available: boolean;
  problems: string[];
  schemaVersion?: number;
  libraryId?: string;
  title?: string;
  packs: Pack[];
  scannedAt: string;
}
