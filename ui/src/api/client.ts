import type { Changed, Commands, Labels, Lang, Me, Meta, Problem, Row, ViewBody, Views } from "./contract";
import { clear } from "../app/cache.svelte";

export class ApiError extends Error {
  constructor(public problem: Problem) {
    super(problem.title);
  }
  get code() {
    return this.problem.code;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: "same-origin",
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401) {
    clear();
    location.href = "/auth/login";
    return new Promise<T>(() => {});
  }
  if (!res.ok) {
    const problem = await res.json().catch(() => ({ status: res.status, title: res.statusText, code: "invalid" }));
    throw new ApiError(problem as Problem);
  }
  return res.json() as Promise<T>;
}

// A literal name is typed by the OpenAPI document; a name read from the meta (string) is not.
type Known<K, M, T, U> = K extends keyof M ? T : U;

const view = <K extends string>(name: K, body: Known<K, Views, ViewBody<Views[K & keyof Views]["params"]>, ViewBody>) =>
  request<Known<K, Views, Views[K & keyof Views]["row"], Row>[]>("POST", `/api/v/${name}`, body);

const call = <K extends string>(name: K, input: Known<K, Commands, Commands[K & keyof Commands]["input"], Record<string, unknown>>) =>
  request<Changed>("POST", `/api/c/${name}`, input);

export const api = {
  me: () => request<Me>("GET", "/api/me"),
  meta: () => request<Meta>("GET", "/api/meta"),
  labels: (lang: Lang) => request<Labels>("GET", `/api/labels/${lang}`),
  view,
  call,
  presign: (b: { hash: string; size: number; type: string }) =>
    request<{ exists: boolean; url?: string; headers?: Record<string, string> }>("POST", "/api/files/presign", b),
  register: (b: { hash: string; name: string; type: string }) => request<{ id: number }>("POST", "/api/files", b),
  file: (b: { view: string; params: Record<string, unknown>; id: number; column: string }) =>
    request<{ url: string; name: string; type: string }>("POST", "/api/file", b),
};
