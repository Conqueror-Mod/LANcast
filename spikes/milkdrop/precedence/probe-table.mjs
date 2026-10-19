// The probe table: each line of probes.txt through MilkDrop 2 (md2ref), stock
// eel-wasm, patched eel-wasm, and the spike's interpreter.
//
//   node probe-table.mjs <md2ref.exe> <stock bc.mjs> <patched bc.mjs> <eel.mjs> <probes.txt>
//
// The two bc.mjs files are copies of butterchurn.min.js with eel-wasm's
// internals exported (README.md, "Running it"). Prints a markdown table.
import fs from "node:fs";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";

const [md2, stock, patched, interp, probes] = process.argv.slice(2);
const here = path.dirname(fileURLToPath(import.meta.url));
const run = (bc) =>
  execFileSync(process.execPath, [path.join(here, "probe3.mjs"), bc, interp, probes])
    .toString().trim().split(/\r?\n/).map((l) => JSON.parse(l));
const a = run(stock), b = run(patched);

const ref = [];
let cur = null;
for (const l of execFileSync(md2, ["-e"], { input: fs.readFileSync(probes) }).toString().split(/\r?\n/)) {
  if (/^V x=/i.test(l)) cur = Number(l.slice(4));
  else if (l.startsWith("ERR")) cur = "ERR";
  else if (l === "END") { ref.push(cur); cur = null; }
}

const f = (v) => (typeof v === "string" ? "parse error" : String(v));
const cell = (v, want) => f(v) + (v !== want ? " ✗" : "");
console.log("| expression | MilkDrop 2 | eel-wasm 0.0.16 | patched | spike interpreter |");
console.log("|---|---|---|---|---|");
a.forEach((r, i) => {
  const e = r.src.replace(/^x = /, "").replace(/;$/, "").replace(/\|/g, "\\|");
  console.log(`| \`${e}\` | ${f(ref[i])} | ${cell(r.eelwasm, ref[i])} | ${cell(b[i].eelwasm, ref[i])} | ${cell(r.interp, ref[i])} |`);
});
