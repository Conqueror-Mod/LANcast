/*
 * The fingerprint a server shows about itself (ADR 0070).
 *
 * It is the out-of-band half of trust on first use, and the only half that
 * lives on this screen. A desktop client meeting this server for the first
 * time shows the key it was offered and asks whether it is right; that
 * question is answerable only by reading the same value off the server
 * somewhere else. If this row is missing, or shows something else, the prompt
 * on the other machine is a formality.
 *
 * jsdom sees no layout, so what this proves is that the value is rendered,
 * grouped for reading aloud, and absent when there is nothing to show.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { Settings } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

// An invented key, never a real one.
const PIN = "AbCdEfGh1234IjKlMnOp5678QrStUvWx9012YzAbCdE=";

let host: HTMLDivElement;
let root: Root;
let fingerprint: string;

beforeEach(() => {
  fingerprint = PIN;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/health")) {
        return json({ status: "ok", version: "0.9.28", api_version: 1 });
      }
      if (url.includes("/api/settings")) {
        return json({
          tmdb: { configured: true },
          opensubtitles: { configured: false },
          omdb: { configured: false },
          rate_per_sec: 5,
          certificate_fingerprint: fingerprint,
        });
      }
      if (url.includes("/api/identity")) {
        return json({
          fingerprint: "AEJE4G6XEG33AUPJU7LGSTT4N74U6D4B",
          fingerprint_display: "AEJE-4G6X-EG33-AUPJ-U7LG-STT4-N74U-6D4B",
          name: "test server",
        });
      }
      if (url.includes("/api/libraries")) return json([]);
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/settings?pane=general"]}>
          <Settings />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  // The settings query is gated on the auth query, so one flush is not enough.
  for (let i = 0; i < 3; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

describe("the server's certificate fingerprint", () => {
  it("shows the identity and the certificate fingerprint, identity first", async () => {
    await render();
    const text = host.textContent ?? "";

    expect(text).toContain("Identity fingerprint");
    expect(text).toContain("TLS certificate fingerprint");

    /*
     * Order matters, and it is the whole correction. The identity is the
     * durable one and the one somebody checking a changed connection key is
     * sent here to read; leading with the value that is reissued for ordinary
     * reasons is what made the first version of this wrong.
     */
    expect(text.indexOf("Identity fingerprint")).toBeLessThan(
      text.indexOf("TLS certificate fingerprint"),
    );
  });

  it("says which of the two changes and which does not", async () => {
    await render();
    const text = host.textContent ?? "";
    expect(text).toContain("does not change when the certificate is");
    expect(text).toContain("reissued for ordinary reasons");
  });

  it("shows the server's own value, grouped for reading aloud", async () => {
    await render();
    const text = host.textContent ?? "";

    // Grouped into runs of eight, which is what makes it readable over a
    // phone — and the same grouping the desktop client uses, so the two
    // screens being compared look alike.
    expect(text).toContain("AbCdEfGh 1234IjKl MnOp5678 QrStUvWx 9012YzAb CdE=");

    // And it is the real value, not a redaction: this one is not a secret,
    // and hiding it would cost the feature its entire point.
    expect(text.replace(/\s/g, "")).toContain(PIN.replace(/\s/g, ""));
  });

  it("says what the certificate fingerprint is for, since nobody arrives knowing", async () => {
    await render();
    const text = host.textContent ?? "";
    expect(text).toContain("another computer");
    expect(text).toContain("Not a secret");
  });

  /*
   * A loopback-only server has no certificate, and the row must not appear
   * empty or as a placeholder. It is also the one server nobody needs to
   * verify — nothing can reach it from another machine.
   */
  it("omits the certificate fingerprint when the server has no certificate", async () => {
    fingerprint = "";
    await render();
    const text = host.textContent ?? "";
    expect(text).not.toContain("TLS certificate fingerprint");
    // The identity is unaffected: a loopback-only server still is somebody.
    expect(text).toContain("Identity fingerprint");
  });
});
