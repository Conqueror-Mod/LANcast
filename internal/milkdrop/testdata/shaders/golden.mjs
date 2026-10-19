// Makes golden.json for shader_test.go from the fixtures here, with the
// spike's JavaScript chain rather than the Go port, so the test holds the port
// to the chain butterchurn's packs were made with (ADR 0091, step g2).
//
// The .milk files beside this are LANcast's own, written for these tests:
// preset licences are uncertain and the repository carries none from a pack.
//
//   node golden.mjs jobs <jobs.jsonl>
//   go run -tags spike ./spikes/milkdrop/shaders/host -wasm <msc.wasm> -in <jobs.jsonl> -out <results.jsonl> -j 1
//   node golden.mjs golden <results.jsonl>          # -> golden.json
//
// <msc.wasm> is internal/milkdrop/msc/msc.wasm.gz, inflated. Not run by the
// test suite; run by hand when a fixture is added or changed.
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const pipeline = path.join(here, "../../../../spikes/milkdrop/shaders/pipeline.mjs");
const { splitShaders, prepareShader, processOptimizedShader } = await import(pathToFileURL(pipeline).href);

const fixtures = fs.readdirSync(here).filter((f) => f.endsWith(".milk")).sort();
const sha = (s) => crypto.createHash("sha256").update(s, "utf8").digest("hex");
const [mode, file] = process.argv.slice(2);

if (mode === "jobs") {
  const out = [];
  for (const f of fixtures) {
    const s = splitShaders(fs.readFileSync(path.join(here, f), "latin1"));
    for (const kind of ["warp", "comp"]) {
      if (s[kind]) out.push(JSON.stringify({ id: `${f}#${kind}`, hlsl: prepareShader(s[kind]) }));
    }
  }
  fs.writeFileSync(file, out.join("\n") + "\n");
} else if (mode === "golden") {
  const results = Object.fromEntries(fs.readFileSync(file, "utf8").trim().split("\n")
    .map((l) => JSON.parse(l)).map((r) => [r.id, r]));
  const golden = {};
  for (const f of fixtures) {
    const s = splitShaders(fs.readFileSync(path.join(here, f), "latin1"));
    const g = {};
    for (const kind of ["warp", "comp"]) {
      if (!s[kind]) continue;
      const r = results[`${f}#${kind}`];
      g[kind] = {
        source: s[kind],
        hlsl_sha256: sha(prepareShader(s[kind])),
        rc: r.rc,
        glsl: r.rc === 0 ? processOptimizedShader(r.glsl) : undefined,
        first_line: r.rc === 0 ? undefined : r.log.split("\n").map((l) => l.trim()).find((l) => l),
      };
    }
    golden[f] = g;
  }
  fs.writeFileSync(path.join(here, "golden.json"), JSON.stringify(golden, null, 1) + "\n");
} else {
  console.error("usage: node golden.mjs jobs <out> | golden <results>");
  process.exit(2);
}
