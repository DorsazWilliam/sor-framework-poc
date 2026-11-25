export type Toast = { id: number; kind: "info" | "success" | "error"; title: string; lines?: string[] };

export const toasts = $state<Toast[]>([]);
let seq = 0;

export function dismiss(id: number) {
  const i = toasts.findIndex((x) => x.id === id);
  if (i >= 0) toasts.splice(i, 1);
}

export function toast(kind: Toast["kind"], title: string, lines?: string[]) {
  const id = ++seq;
  toasts.push({ id, kind, title, lines });
  setTimeout(() => dismiss(id), kind === "error" ? 8000 : 4000);
}
