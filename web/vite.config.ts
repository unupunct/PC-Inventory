import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Builds straight into the Go embed directory: internal/server/webui/dist
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/server/webui/dist",
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    proxy: { "/api": "http://127.0.0.1:8080" },
  },
});
