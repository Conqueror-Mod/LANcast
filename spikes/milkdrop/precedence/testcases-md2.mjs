// Run eel-wasm's own test cases (tools/testCases.ts, which its author checked in
// real MilkDrop via buildTestPreset.js) through md2ref, and report agreement.
// argv: <md2ref.exe> <testCases.ts>
import fs from "node:fs"; import { execFileSync } from "node:child_process";
const src = fs.readFileSync(process.argv[3], "utf8");
const arr = src.slice(src.indexOf("= [") + 2, src.lastIndexOf("]") + 1);
const cases = new Function("return " + arr)(); // our own repo-pinned file, data only
const skip = /megabuf|gmegabuf|loop|while|rand|exec3|sigmoid|invsqrt|log|exp|tan|atan|asin|acos|ceil|sign|memset|memcpy|freembuf|reg\d|\$|\[|\?|~|<<|>>|int\(/i;
let ran = 0, agree = 0; const bad = [], skipped = [];
for (const [desc, code, expected] of cases) {
  if (skip.test(code)) { skipped.push(desc); continue; }
  const out = execFileSync(process.argv[2], ["-e"], { input: (/(^|[^a-z_])x([^a-z_0-9]|$)/i.test(code) ? "x = 10; " : "") + code.replace(/\n/g, " ") + "\n" }).toString();
  if (/^ERR/m.test(out)) { bad.push({ desc, code, expected, got: "parse error" }); ran++; continue; }
  const m = out.match(/^V g=(.*)$/mi); const got = m ? Number(m[1]) : 0;
  ran++; if (Math.abs(got - expected) < 1e-9 || (Number.isNaN(got) && Number.isNaN(expected))) agree++; else bad.push({ desc, code, expected, got });
}
console.log(JSON.stringify({ total: cases.length, ran, agree, skipped: skipped.length, disagree: bad }, null, 1));
