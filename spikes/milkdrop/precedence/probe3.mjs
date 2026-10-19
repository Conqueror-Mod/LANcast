// Run one-line probes through the vendored eel-wasm (compileModule lifted out of
// butterchurn.min.js by an appended export) and through the spike's interpreter.
// argv: <bc.mjs copy> <spike eel.mjs> <probes.txt>
import fs from "node:fs"; import { pathToFileURL } from "node:url";
globalThis.window = globalThis;
const bc = await import(pathToFileURL(process.argv[2]).href);
const { equations } = await import(pathToFileURL(process.argv[3]).href);
for (const src of fs.readFileSync(process.argv[4], "utf8").split(/\r?\n/).filter(Boolean)) {
  let ew, it;
  try {
    const g = { x: new WebAssembly.Global({ value: "f64", mutable: true }, 0) };
    const m = await WebAssembly.instantiate(await WebAssembly.compile(bc.__compileModule({ pools: { p: new Set(["x"]) }, functions: { f: { pool: "p", code: src } } })), { p: g, shims: bc.__shims });
    m.exports.f(); ew = g.x.value;
  } catch (e) { ew = "ERR " + e.message.split("\n")[0]; }
  try { it = equations(src)({}).x; } catch (e) { it = "ERR " + e.message.split("\n")[0]; }
  console.log(JSON.stringify({ src, eelwasm: ew, interp: it }));
}
