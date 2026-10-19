// Does LANcast's Go validator (internal/milkdrop/eel) accept exactly the EEL
// blocks eel-wasm 0.0.17 accepts? Reads the blocks the Go corpus test wrote
// (LANCAST_MILKDROP_DIFF) and compiles each one alone with eel-wasm.
//
//   node godiff.mjs <eel-wasm dist/index.js> <blocks.jsonl> [extra strings...]
//
// Any disagreement is a bug in the validator: accepting what eel-wasm refuses
// means a preset that is black on every screen, and refusing what it accepts
// means a working preset turned away. Limits are the one intended difference
// (a block eel-wasm compiles can still be refused for its size or nesting);
// they are reported apart.
import fs from "node:fs";
import readline from "node:readline";
import { pathToFileURL } from "node:url";

const [lib, blocksFile] = process.argv.slice(2);
const ew = await import(pathToFileURL(lib).href);

function wasmOK(src) {
  try {
    ew.compileModule({ pools: { a: new Set() }, functions: { f: { pool: "a", code: src } }, eelVersion: 2 });
    return [true, ""];
  } catch (e) {
    return [false, String(e.message).split("\n")[0]];
  }
}

const limit = /nested more than|loops are nested|the block is \d+ bytes|runs at most/;
let n = 0, agree = 0, limited = 0;
const wrong = [];
const rl = readline.createInterface({ input: fs.createReadStream(blocksFile) });
for await (const line of rl) {
  if (!line) continue;
  const b = JSON.parse(line);
  n++;
  const [ok, msg] = wasmOK(b.src);
  if (ok === b.ok) { agree++; continue; }
  if (ok && limit.test(b.msg)) { limited++; continue; }
  wrong.push({ file: b.file.split(/[\\/]/).pop(), block: b.block, go: b.ok ? "accept" : b.msg, wasm: ok ? "accept" : msg, src: b.src.slice(0, 160) });
}
console.log(JSON.stringify({ blocks: n, agree, refusedForALimitOnly: limited, disagree: wrong.length, examples: wrong.slice(0, 25) }, null, 1));
