import type { Changed, Commands, Labels, Lang, Me, Meta, Problem, ViewBody, Views } from "./contract";

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
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401) {
    location.href = "/auth/login";
    return new Promise<T>(() => {});
  }
  if (!res.ok) {
    const problem = await res.json().catch(() => ({ status: res.status, title: res.statusText, code: "invalid" }));
    throw new ApiError(problem as Problem);
  }
  return res.json() as Promise<T>;
}

export const api = {
  me: () => request<Me>("GET", "/api/me"),
  meta: () => request<Meta>("GET", "/api/meta"),
  labels: (lang: Lang) => request<Labels>("GET", `/api/labels/${lang}`),
  view: <K extends keyof Views & string>(name: K, body: ViewBody<Views[K]["params"]>) =>
    request<Views[K]["row"][]>("POST", `/api/v/${name}`, body),
  call: <K extends keyof Commands & string>(name: K, input: Commands[K]["input"]) =>
    request<Changed>("POST", `/api/c/${name}`, input),
  presign: (b: { hash: string; size: number; type: string }) =>
    request<{ exists: boolean; url?: string; headers?: Record<string, string> }>("POST", "/api/files/presign", b),
  register: (b: { hash: string; name: string; type: string }) => request<{ id: number }>("POST", "/api/files", b),
  file: (b: { view: string; params: Record<string, unknown>; id: number; column: string }) =>
    request<{ url: string; name: string; type: string }>("POST", "/api/file", b),
};
