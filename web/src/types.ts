// Mirrors internal/web/view's row/group json tags verbatim.

export interface Row {
  url: string;
  title: string;
  state: string;
  glyph: string;
  unattended: boolean;
  alive: boolean;
  blocking: string[] | null;
  after: string[] | null;
  reason: string;
  elapsed: string;
  pr_number: number;
}

export interface Group {
  root: Row | null;
  children: Row[];
}
