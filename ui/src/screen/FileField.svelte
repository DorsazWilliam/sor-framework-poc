<script lang="ts">
  import Upload from "~icons/fluent/arrow-upload-16-regular";
  import Button from "../ds/Button.svelte";
  import { api } from "../api/client";
  import { t } from "../labels/i18n.svelte";

  // SPEC §11: sha256 → presign → PUT → register → id; preview through POST /api/file.
  let {
    value = $bindable(null),
    source,
    readonly = false,
    id,
  }: {
    value?: number | null;
    source?: { view: string; params: Record<string, unknown>; id: number; column: string };
    readonly?: boolean;
    id?: string;
  } = $props();

  let input = $state<HTMLInputElement>();
  let busy = $state(false);
  let preview = $state<{ url: string; name: string; type: string } | null>(null);

  $effect(() => {
    if (value == null || !source || preview) return;
    api.file(source).then((p) => (preview = p), () => {});
  });

  async function upload(f: File) {
    busy = true;
    try {
      const digest = await crypto.subtle.digest("SHA-256", await f.arrayBuffer());
      const hash = [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
      const type = f.type || "application/octet-stream";
      const p = await api.presign({ hash, size: f.size, type });
      if (!p.exists) await fetch(p.url!, { method: "PUT", headers: p.headers, body: f });
      value = (await api.register({ hash, name: f.name, type })).id;
      preview = { url: URL.createObjectURL(f), name: f.name, type };
    } finally {
      busy = false;
    }
  }
</script>

<div class="file">
  {#if !readonly}
    <input bind:this={input} {id} type="file" hidden onchange={(e) => e.currentTarget.files?.[0] && upload(e.currentTarget.files[0])} />
    <Button icon={Upload} disabled={busy} onclick={() => input?.click()}>{t(value == null ? "ui.upload" : "ui.replace")}</Button>
  {/if}
  {#if preview}
    <a href={preview.url} target="_blank" rel="noreferrer">{preview.name}</a>
    {#if preview.type.startsWith("image/")}
      <img src={preview.url} alt={preview.name} />
    {:else if preview.type === "application/pdf"}
      <iframe src={preview.url} title={preview.name}></iframe>
    {/if}
  {:else if value != null}
    <span class="muted">#{value}</span>
  {/if}
</div>

<style>
  .file {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--s-3);
  }
  img,
  iframe {
    width: 100%;
    border: var(--bw) solid var(--border);
    border-radius: var(--r-input);
  }
  img {
    max-height: 320px;
    object-fit: contain;
  }
  iframe {
    height: 420px;
  }
</style>
