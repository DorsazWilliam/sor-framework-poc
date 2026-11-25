// JSON contract of SPEC §13. Views and commands are typed by name from the OpenAPI document (src/gen/api.d.ts).
import type { components, operations } from "../gen/api";

// The server's types, generated from its OpenAPI document (src/gen/api.d.ts): GET /api/me, GET /api/meta and the
// bodies of views and commands. Nothing of them is written here.
type S = components["schemas"];

export type Me = S["Me"];
export type Lang = Me["lang"];
export type Meta = S["Meta"];
export type Table = S["Table"];
export type View = S["View"];
export type Command = S["Command"];
export type Screen = S["Screen"];
export type Col = S["Col"];
export type TableCol = S["TableCol"];
export type Format = Col["format"];
export type Wrap = S["Wrap"];
export type Sort = S["Sort"];
export type Agg = S["Agg"];
// A filter on a column, or by an action: the rows where its conditions are true (SPEC §7).
export type Filter = NonNullable<Wrap["filter"]>[number];
export type ColFilter = Extract<Filter, { col: string }>;
export type Op = ColFilter["op"];
// `call` is the `e` of the live keys this command produces.
export type Changed = S["Result"];
export type Rejected = S["Rejected"];
export type Problem = S["Problem"];

export type Row = Record<string, unknown>;

export type ViewBody<P = Record<string, unknown>> = { params?: P } & Wrap;

// A key of the live stream (SSE, outside the OpenAPI document).
export type LiveKey = { t: string; ids?: number[]; c: string; e: string };

export type Labels = Record<string, string>;

// Views and commands by registry name, from the operations view_<name> and command_<name>.
type Ops<P extends string> = { [K in keyof operations as K extends `${P}${infer N}` ? N : never]: operations[K] };
type Body<O> = O extends { requestBody?: { content: { "application/json": infer B } } } ? B : never;
type Rows<O> = O extends { responses: { 200: { content: { "application/json": (infer R)[] } } } } ? R : never;

export type Views = { [N in keyof Ops<"view_">]: { params: NonNullable<Body<Ops<"view_">[N]>["params"]>; row: Rows<Ops<"view_">[N]> } };
export type Commands = { [N in keyof Ops<"command_">]: { input: Body<Ops<"command_">[N]> } };
