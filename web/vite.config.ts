import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";

// The client is served embedded in the Go binary, so it builds into the Go
// web package's dist/ directory (embedded via //go:embed). In development the
// Vite server proxies the API to the running lancastd on :8080, giving hot
// reload against real data.
//
// A server with a password set serves HTTPS with a self-signed certificate and
// redirects http to it, so the plain default proxy follows the redirect into a
// certificate the browser will not accept and every API call fails with
// ERR_CERT_AUTHORITY_INVALID -- which looks like a client that renders nothing
// rather than like a proxy problem. `secure: false` is what lets the *proxy*
// accept that certificate; it is a dev-server setting and has nothing to do
// with what the built client trusts. Point LANCAST_API at the https origin to
// develop against a secured server.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      /*
       * hls.js resolves to the vendored bundle, never to a registry.
       *
       * It is not in package.json on purpose: ADR 0013 requires this library to
       * be the artefact reproduced and checked in under web/vendor, not
       * whatever npm would install at build time. An alias is what makes
       * `import("hls.js")` mean that file and nothing else.
       */
      "hls.js": fileURLToPath(
        new URL("./vendor/hls.js/hls.min.js", import.meta.url),
      ),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: process.env.LANCAST_API ?? "http://127.0.0.1:8080",
        changeOrigin: true,
        secure: false,
      },
    },
  },
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
  },
});
