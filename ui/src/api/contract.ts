// JSON contract of SPEC §13. Replaced or narrowed by openapi-typescript output in src/gen/.

export type Lang = "fr" | "de" | "en";

export type Me = {
  id: number; name: string; kind: string; lang: Lang;
  functions: string[]; reach: Record<string, number[]>; for?: { id: number; name: string };
};

export type Format = "" | "min" | "tod" | "pct" | "coef" | "money";

export type Col = {
  name: string; type: string; format: Format; nullable: boolean;
  ref?: string; enum?: string; description?: string;
};

export type TableCol = Col & { guarded: boolean; has_default: boolean };

export type Table = {
  name: string; root: string; child_fk?: string; under?: string;
  anchor?: { column: string; reach: string }; read: string; write: string; label: string; crud: boolean;
  columns: TableCol[]; conditions: string[]; actions: Record<string, string[]>; description?: string;
};

export type View = {
  name: string; params: Col[]; columns: Col[]; roots: string[]; needs: string;
  anchor?: { column: string; reach: string }; description?: string;
};

export type Command = {
  name: string; table: string; kind: "create" | "update" | "delete" | "custom" | "run";
  rootless: boolean; input: Col[]; when?: string; ensure?: string; reach?: string; needs: string; description?: string;
};

export type Op = "eq" | "ne" | "lt" | "le" | "gt" | "ge" | "in" | "contains" | "null" | "notnull";
export type Filter = { col: string; op: Op; value?: unknown };
export type Sort = { col: string; dir: "asc" | "desc" };
export type Agg = { col: string; fn: "sum" | "count" | "min" | "max" | "avg" };

export type Wrap = {
  filter?: Filter[]; sort?: Sort[]; group?: string[]; agg?: Agg[]; limit?: number; offset?: number;
};

export type Screen = {
  id: string; group: string; views: string[]; defaults: Record<string, string>; table?: string;
  commands: string[]; wrap?: Wrap; children: Screen[]; link?: Record<string, string>; component?: string;
};

export type Meta = {
  tables: Table[]; views: View[]; commands: Command[]; screens: Screen[];
  enums: Record<string, string[]>; functions: string[]; groups: string[];
};

export type Row = Record<string, unknown>;

export type ViewBody<P = Record<string, unknown>> = { params?: P } & Wrap;

export type Changed = { changed: { table: string; ids: number[] }[]; ids: number[]; run?: number };

export type Rejected = { id: number; conditions: string[]; row: Row };

export type Problem = {
  status: number; title: string; detail?: string;
  code: "unauthenticated" | "forbidden" | "not_found" | "rejected" | "constraint" | "invalid";
  needs?: string; action?: string; rejected?: Rejected[]; constraint?: string; errors?: unknown[];
};

export type LiveKey = { t: string; ids?: number[]; c: string; e: string };

export type Labels = Record<string, string>;

// Name-indexed maps; generated types narrow them per view and command.
export interface Views { [name: string]: { params: Record<string, unknown>; row: Row } }
export interface Commands { [name: string]: { input: Record<string, unknown> } }
