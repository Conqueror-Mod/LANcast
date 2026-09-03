import { describe, expect, it } from "vitest";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import openapiTS, { astToString } from "openapi-typescript";

/*
 * src/api/schema.ts against docs/openapi.json.
 *
 * schema.ts is generated, and a generated file that is committed is a file that
 * can go stale. The Go side (internal/api/openapi_test.go) proves the spec
 * matches the router; this proves the client types match the spec. Between
 * them there is no gap for drift to live in — which is the whole reason the
 * spec exists, since docs/api.md drifting from the handlers is the failure this
 * project has paid for repeatedly.
 *
 * It regenerates rather than diffing text, so reformatting or an
 * openapi-typescript upgrade shows up here as a real difference rather than a
 * false one.
 */

// Resolved from the working directory rather than import.meta.url: under
// vitest's jsdom environment import.meta.url is not a file: URL, and vitest
// always runs with web/ as the cwd.
const specPath = resolve(process.cwd(), "../docs/openapi.json");
const generatedPath = resolve(process.cwd(), "src/api/schema.ts");

describe("generated API schema", () => {
  it("is up to date with docs/openapi.json", async () => {
    // Read and parse rather than handing openapiTS a path: under jsdom it
    // answers a file: URL by trying to fetch it. Every $ref in the document is
    // local, so there is nothing left for it to resolve anyway.
    const spec = JSON.parse(await readFile(specPath, "utf8"));
    const ast = await openapiTS(spec);
    const expected = astToString(ast);
    const actual = await readFile(generatedPath, "utf8");

    expect(
      normalize(actual),
      "src/api/schema.ts is out of date with docs/openapi.json. " +
        "Run `npm run openapi` in web/ and commit the result.",
    ).toBe(normalize(expected));
  });
});

/*
 * Two differences that are not drift:
 *
 * Line endings — the repository normalises them via .gitattributes, but a
 * checkout can still hand us CRLF.
 *
 * The "auto-generated" banner — openapi-typescript's CLI writes it and its
 * JavaScript API does not, so the committed file has it and the freshly
 * generated comparison never will. Stripping it is not weakening the check:
 * everything the banner precedes is still compared exactly.
 */
function normalize(s: string): string {
  return s
    .replace(/\r\n/g, "\n")
    .replace(/^\/\*\*[\s\S]*?\*\/\n+/, "")
    .trimEnd();
}
