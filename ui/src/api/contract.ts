// JSON contract of SPEC §13. Views and commands are typed by name from the OpenAPI document (src/gen/api.d.ts).
import type { operations } from "../gen/api";

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

// `call` is the `e` of the live keys this command produces.
export type Changed = { changed: { table: string; ids: number[] }[]; ids: number[]; run?: number; call: string };

export type Rejected = { id: number; conditions: string[]; row: Row };

export type Problem = {
  status: number; title: string; detail?: string;
  code: "unauthenticated" | "forbidden" | "not_found" | "rejected" | "constraint" | "invalid";
  needs?: string; action?: string; rejected?: Rejected[]; constraint?: string; errors?: unknown[];
};

export type LiveKey = { t: string; ids?: number[]; c: string; e: string };

export type Labels = Record<string, string>;

// Views and commands by registry name, from the operations view_<name> and command_<name>.
type Ops<P extends string> = { [K in keyof operations as K extends `${P}${infer N}` ? N : never]: operations[K] };
type Body<O> = O extends { requestBody?: { content: { "application/json": infer B } } } ? B : never;
type Rows<O> = O extends { responses: { 200: { content: { "application/json": (infer R)[] } } } } ? R : never;

export type Views = { [N in keyof Ops<"view_">]: { params: NonNullable<Body<Ops<"view_">[N]>["params"]>; row: Rows<Ops<"view_">[N]> } };
export type Commands = { [N in keyof Ops<"command_">]: { input: Body<Ops<"command_">[N]> } };
