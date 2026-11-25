import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import Icons from "unplugin-icons/vite";
import { mock } from "./dev/mock.ts";

export default defineConfig({
  plugins: [svelte(), Icons({ compiler: "svelte" }), mock()],
  server: { port: 5173, strictPort: true },
});
