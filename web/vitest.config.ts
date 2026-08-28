import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      /*
       * The same alias the real build uses, for the same reason.
       *
       * Without it every test that renders the live screen fails to resolve
       * `hls.js` and reports as a broken import rather than as anything about
       * the code — and the tests that matter most here are precisely the ones
       * asserting the library is *not* reached.
       */
      "hls.js": fileURLToPath(
        new URL("./vendor/hls.js/hls.min.js", import.meta.url),
      ),
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
