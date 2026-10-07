// Mirrors internal/web/view's row/group json tags verbatim: GET /graph.json marshals
// []group directly, so this is the graph island's only view of the data too
// (docs/prds/prd-fleet-view.md § One derivation).

export interface Row {
  url: string;
  title: string;
  state: string;
  tone: string;
  unattended: boolean;
  alive: boolean;
  blocking: string[] | null;
}

export interface Group {
  root: Row | null;
  children: Row[];
}
