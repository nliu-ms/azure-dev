import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { resolve } from "node:path";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: resolve(__dirname, "../internal/migrationweb/assets"),
    emptyOutDir: true,
    sourcemap: false,
  },
});
