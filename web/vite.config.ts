import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";

// The client is served embedded in the Go binary, so it builds into the Go
// web package's dist/ directory (embedded via //go:embed). In development the
// Vite server proxies the API to the running lancastd on :8080, giving hot
// reload against real data.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://127.0.0.1:8080", changeOrigin: true },
    },
  },
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
  },
});
