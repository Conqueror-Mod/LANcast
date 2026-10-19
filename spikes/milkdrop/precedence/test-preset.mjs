// Write a MilkDrop 2 test preset for the precedence cases, in exactly the form
// eel-wasm's tools/buildTestPreset.js produces (that script cannot run as
// shipped: it require()s an ES-module .ts file). Load the result in Winamp's
// MilkDrop 2: green means every case holds there; red means one failed, and
// the `monitor` value (press N) is its 1-based number in the list below.
//
//   node test-preset.mjs <eel-wasm>/packages/compiler/tools/testCases.ts > precedence-test.milk
//
// Only the precedence cases are taken (the author's seven and the sixteen this
// step added), so the per-frame block stays far below MilkDrop's line limit.
import fs from "node:fs";

const src = fs.readFileSync(process.argv[2], "utf8");
const all = new Function("return " + src.slice(src.indexOf("= [") + 2, src.lastIndexOf("]") + 1))();
const cases = all.filter(([d]) => /precedence|associativ|looser|tighter|signed right operand/i.test(d));

const lines = [`q1 = 1;`, `monitor = -1;`];
cases.forEach(([description, code, expected], i) => {
  lines.push(`// ${i + 1}: ${description}`);
  lines.push(`g = 0; // Reset`);
  lines.push(`x = 10; // Reset`);
  lines.push(code + ";");
  lines.push(`pass = g == ${expected};`);
  lines.push(`monitor = if(!pass && monitor == -1, ${i + 1}, monitor);`);
  lines.push(`q1 = q1 && pass;`);
});
const perFrame = lines.map((line, i) => `per_frame_${i + 1}=${line}`).join("\n");

console.error(cases.map(([d, c, e], i) => `${i + 1}. ${d}: ${c} -> ${e}`).join("\n"));
console.log(`MILKDROP_PRESET_VERSION=201
PSVERSION=2
PSVERSION_WARP=2
PSVERSION_COMP=2
[preset00]
${perFrame}
warp_1=\`shader_body
warp_2=\`{
warp_3=\`    // sample previous frame
warp_4=\`    ret = tex2D( sampler_main, uv ).xyz;
warp_5=\`}
comp_1=\`shader_body
comp_2=\`{
comp_3=\`    if(q1) {
comp_4=\`      ret = float3(0,1,0);
comp_5=\`    } else {
comp_6=\`      ret = float3(1,0,0);
comp_7=\`    }
comp_8=\`}`);
