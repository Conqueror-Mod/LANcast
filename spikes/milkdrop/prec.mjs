// SPIKE — operator precedence of & and | : interpreter vs eel-wasm, on one line.
import path from "node:path"; import { pathToFileURL } from "node:url"; import { equations } from "./eel.mjs";
const ew = await import(pathToFileURL(path.resolve(process.argv[2], "eel-wasm-0.0.17/package/dist/index.js")).href);
for (const src of ["x = 5 - 3 & 6;", "x = 1 + 2 | 4;", "x = 2 * 3 & 7;", "x = -2 ^ 2;", "x = 2 ^ 3 ^ 2;", "x = 7 % 4 * 2;"]) {
  const a = equations(src)({});
  const g = { x: new WebAssembly.Global({ value: "f64", mutable: true }, 0) };
  const m = await WebAssembly.instantiate(await WebAssembly.compile(ew.compileModule({ pools: { p: new Set(["x"]) }, functions: { f: { pool: "p", code: src } } })), { p: g, shims: ew.shims });
  m.exports.f();
  console.log(src.padEnd(20), "interp", a.x, " eel-wasm", g.x.value);
}
