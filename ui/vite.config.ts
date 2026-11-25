import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import Icons from "unplugin-icons/vite";

// Dev server: API, live stream and login go to the running stack (SOR_URL, default http://localhost:8080).
const target = process.env.SOR_URL ?? "http://localhost:8080";

export default defineConfig({
  plugins: [svelte(), Icons({ compiler: "svelte" })],
  server: { port: 5173, strictPort: true, proxy: { "/api": target, "/auth": target } },
});
