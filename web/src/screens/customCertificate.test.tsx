/*
 * Server › Network: a certificate of your own (ADR 0088). What is asserted is
 * the wiring and the words: the status line is about what other devices are
 * actually served, a refused pair comes back as the server's sentence, and
 * saving sends the paths and refreshes the Network status beside it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Settings } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

const generated = {
  available: true,
  cert_file: "",
  key_file: "",
  serving: "generated",
  subject: "LANcast",
  issuer: "LANcast",
  names: ["localhost", "192.168.1.20"],
  not_after: "2036-01-01T00:00:00Z",
  self_signed: true,
  fingerprint: "AAAA",
};
const custom = {
  ...generated,
  cert_file: "C:/Certs/media.pem",
  key_file: "C:/Certs/media-key.pem",
  serving: "custom",
  subject: "media.home.example",
  issuer: "Home CA",
  names: ["media.home.example"],
  self_signed: false,
  fingerprint: "BBBB",
};

let cert: Record<string, unknown>;
let calls: { url: string; method: string; body?: string }[];
let putAnswer: { status: number; body: unknown };

function mount(initial: Record<string, unknown>) {
  cert = initial;
  calls = [];
  putAnswer = { status: 200, body: custom };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ url, method, body: init?.body as string | undefined });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "dana", role: "admin" } });
      }
      if (url.includes("/api/settings/custom-certificate")) {
        if (method === "PUT") {
          if (putAnswer.status === 200) cert = putAnswer.body as Record<string, unknown>;
          return json(putAnswer.body, putAnswer.status);
        }
        if (method === "DELETE") {
          cert = generated;
          return json(generated);
        }
        return json(cert);
      }
      if (url.includes("/api/network")) {
        return json({ port: "8080", lan_enabled: true, secured: true, restart_required: false,
          addresses: [], certificate_fingerprint: cert.fingerprint, custom_certificate: cert.serving === "custom" });
      }
      return json({});
    }),
  );
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/settings"]}>
            <Settings />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
  const pane = [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === "Network");
  expect(pane).toBeTruthy();
  await act(async () => pane!.click());
  await settle();
}

function section() {
  const s = [...host.querySelectorAll(".settings__section")].find((el) =>
    el.textContent?.includes("Your own certificate"),
  );
  expect(s).toBeTruthy();
  return s as HTMLElement;
}

function field(label: string) {
  const row = [...section().querySelectorAll("label")].find((l) => l.textContent?.includes(label));
  return row!.querySelector("input") as HTMLInputElement;
}

function type(input: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

function button(text: string) {
  return [...section().querySelectorAll("button")].find((b) => b.textContent?.includes(text));
}

describe("a certificate of your own", () => {
  it("says the generated certificate is being served, and offers no way back to it", async () => {
    mount(generated);
    await render();
    expect(section().textContent).toContain("Other devices are served the certificate LANcast made");
    expect(button("Use LANcast")).toBeUndefined();
    expect(button("Use this certificate")!.disabled).toBe(true);
  });

  it("sends both paths and shows what is now served", async () => {
    mount(generated);
    await render();
    type(field("Certificate file"), " C:/Certs/media.pem ");
    type(field("Key file"), "C:/Certs/media-key.pem");
    await act(async () => button("Use this certificate")!.click());
    await settle();

    const put = calls.find((c) => c.method === "PUT");
    expect(JSON.parse(put!.body!)).toEqual({ cert_file: "C:/Certs/media.pem", key_file: "C:/Certs/media-key.pem" });
    expect(section().textContent).toContain("served your certificate for media.home.example");
    expect(section().textContent).toContain("From Home CA");
    // The fingerprint beside it is refetched, not left describing the old one.
    const networkGets = calls.filter((c) => c.url.includes("/api/network"));
    expect(networkGets.length).toBeGreaterThan(1);
    expect(host.textContent).toContain("BBBB");
  });

  it("shows the server's reason when a pair is refused", async () => {
    mount(generated);
    await render();
    putAnswer = { status: 400, body: { error: { code: "bad_request",
      message: "the certificate and key aren't a usable pair: tls: private key does not match public key" } } };
    type(field("Certificate file"), "C:/Certs/a.pem");
    type(field("Key file"), "C:/Certs/b-key.pem");
    await act(async () => button("Use this certificate")!.click());
    await settle();
    expect(section().textContent).toContain("aren't a usable pair");
  });

  it("says why a configured certificate is not being served", async () => {
    mount({ ...generated, cert_file: "C:/Certs/media.pem", key_file: "C:/Certs/media-key.pem",
      error: "the certificate expired on 2026-01-01" });
    await render();
    expect(section().textContent).toContain("The certificate expired on 2026-01-01.");
    expect(section().textContent).toContain("until this is fixed");
  });

  it("goes back to LANcast's certificate", async () => {
    mount(custom);
    await render();
    await act(async () => button("Use LANcast")!.click());
    await settle();
    expect(calls.some((c) => c.method === "DELETE" && c.url.includes("custom-certificate"))).toBe(true);
    expect(section().textContent).toContain("the certificate LANcast made");
  });

  it("says there is nothing to put a certificate on when the server answers this computer only", async () => {
    mount({ ...generated, available: false, serving: "none" });
    await render();
    expect(section().textContent).toContain("serves no HTTPS");
    expect(section().querySelectorAll("input")).toHaveLength(0);
  });
});
