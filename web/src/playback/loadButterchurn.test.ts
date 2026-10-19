import { describe, expect, it, vi } from "vitest";
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { loadButterchurn } from "./loadButterchurn";

/*
 * The vendored MilkDrop engine (ADR 0091, step a).
 *
 * What these prove is provenance and wiring, not rendering: jsdom has no
 * WebGL, so nothing here draws a frame. They prove that the name
 * `butterchurn` can only mean the file in web/vendor, that the file is the
 * one the README's reproduction recorded, and that the JavaScript preset path
 * is not in it.
 */

// Resolved from the working directory, as schema.test.ts does: vitest always
// runs with web/ as the cwd.
const vendored = resolve(process.cwd(), "vendor/butterchurn/butterchurn.min.js");
const readme = resolve(process.cwd(), "vendor/butterchurn/README.md");

/*
 * The alias a config file gives `butterchurn`, resolved against web/.
 *
 * Read as text rather than imported: importing vite.config.ts under jsdom pulls
 * in esbuild, which refuses to start in a jsdom realm. The pattern is the one
 * both files use for every vendored library, so a rewrite that changed the
 * shape fails here loudly rather than passing quietly.
 */
function aliasIn(configFile: string): string | undefined {
  const text = readFileSync(resolve(process.cwd(), configFile), "utf8");
  const rel = text.match(
    /\bbutterchurn:\s*fileURLToPath\(\s*new URL\("([^"]+)",\s*import\.meta\.url\)/,
  )?.[1];
  return rel === undefined ? undefined : resolve(process.cwd(), rel);
}

describe("vendored butterchurn", () => {
  it("is what the name resolves to, in the build and in the tests", () => {
    expect(aliasIn("vite.config.ts")).toBe(vendored);
    expect(aliasIn("vitest.config.ts")).toBe(vendored);
    expect(existsSync(vendored)).toBe(true);
  });

  it("is not a registry dependency", () => {
    // The alias is only a guarantee while there is nothing else for the name
    // to mean. A package.json entry would put upstream's build, with its
    // `new Function` path, one alias edit away.
    const pkg = JSON.parse(readFileSync(resolve(process.cwd(), "package.json"), "utf8"));
    expect(pkg.dependencies?.butterchurn).toBeUndefined();
    expect(pkg.devDependencies?.butterchurn).toBeUndefined();
    expect(pkg.dependencies?.["eel-wasm"]).toBeUndefined();
  });

  it("matches the sha256 its README records", () => {
    // The recorded build has both of LANcast's patches in it: no-js-presets
    // (butterchurn) and eel-wasm-precedence (the compiler it bundles). A
    // rebuild that leaves either out comes out with a different hash.
    const actual = createHash("sha256").update(readFileSync(vendored)).digest("hex");
    const recorded = readFileSync(readme, "utf8").match(
      /\| sha256 \(vendored\) \| `([0-9a-f]{64})` \|/,
    )?.[1];
    expect(recorded, "README.md has no `sha256 (vendored)` row").toBeDefined();
    expect(actual, "the artefact and its README disagree; redo the reproduction").toBe(recorded);
  });

  it("carries the precedence patch the README records", () => {
    // ADR 0091 step f′: eel-wasm 0.0.16 groups `&`, `|`, `!` and `&&` unlike
    // MilkDrop, and 110 corpus presets compute something their authors never
    // saw. The patch is not visible in the minified artefact (it changes only
    // jison's LR table), so the artefact hash above is what proves it was
    // applied; this proves the patch it was built with is the one on disk.
    const patch = resolve(process.cwd(), "vendor/butterchurn/eel-wasm-precedence.patch");
    expect(existsSync(patch)).toBe(true);
    const actual = createHash("sha256").update(readFileSync(patch)).digest("hex");
    const recorded = readFileSync(readme, "utf8").match(
      /\| sha256 \(eel-wasm-precedence\.patch\) \| `([0-9a-f]{64})` \|/,
    )?.[1];
    expect(recorded, "README.md has no `sha256 (eel-wasm-precedence.patch)` row").toBeDefined();
    expect(actual, "the patch and its README disagree; redo the reproduction").toBe(recorded);
  });

  it("has no way to turn a string into code", () => {
    // The whole of ADR 0091 rests on this: a preset's equations become
    // WebAssembly or nothing. Upstream's bundle has eight `new Function` calls,
    // all in loadJSPreset; LANcast's build removes that method
    // (no-js-presets.patch). An upgrade that forgets the patch fails here.
    const src = readFileSync(vendored, "utf8");
    expect(src).not.toMatch(/\bFunction\s*\(/);
    expect(src).not.toMatch(/\beval\s*\(/);
    expect(src).not.toMatch(/setTimeout|setInterval/);
    expect(src).not.toContain("loadJSPreset");
    expect(src).not.toContain("init_eqs_str");
  });

  it("makes no network or storage calls of its own", () => {
    const src = readFileSync(vendored, "utf8");
    for (const api of [
      "fetch(",
      "XMLHttpRequest",
      "WebSocket",
      "EventSource",
      "sendBeacon",
      "localStorage",
      "sessionStorage",
      "indexedDB",
      "document.cookie",
      "importScripts",
    ]) {
      expect(src, api).not.toContain(api);
    }
  });

  it("loads through the dynamic import, and always with onlyUseWASM", async () => {
    const mod = await import("butterchurn");
    expect(typeof mod.default.createVisualizer).toBe("function");

    const spy = vi
      .spyOn(mod.default, "createVisualizer")
      .mockReturnValue({} as ReturnType<typeof mod.default.createVisualizer>);
    try {
      const engine = await loadButterchurn();
      const canvas = document.createElement("canvas");
      const ctx = {} as AudioContext;

      // A caller cannot switch it off: the type forbids it, and the value is
      // overwritten even if a cast smuggles it in.
      engine.createVisualizer(ctx, canvas, {
        width: 640,
        onlyUseWASM: false,
      } as Parameters<typeof engine.createVisualizer>[2]);
      engine.createVisualizer(ctx, canvas);

      expect(spy).toHaveBeenNthCalledWith(1, ctx, canvas, { width: 640, onlyUseWASM: true });
      expect(spy).toHaveBeenNthCalledWith(2, ctx, canvas, { onlyUseWASM: true });
    } finally {
      spy.mockRestore();
    }
  });
});
