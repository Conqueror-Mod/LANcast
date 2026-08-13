/*
 * When to tell somebody their window is out of date.
 *
 * The check exists because the in-app updater cannot replace a running client:
 * a fullscreen fix shipped inside LANcast-Client.exe, the server updated
 * itself, and the button went on doing nothing because the window was older
 * than the binary on disk. Nothing said so.
 *
 * The interesting half is the *silence*: this banner interrupts everybody, so
 * every case where it is unsure has to stay quiet rather than nag.
 */
import { describe, it, expect } from "vitest";
import { clientIsStale } from "./clientVersion";

describe("clientIsStale", () => {
  it("notices a window older than its server", () => {
    expect(clientIsStale({ client_version: "0.6.19" }, "0.6.20")).toBe(true);
  });

  it("says nothing when they match, tag prefix or not", () => {
    expect(clientIsStale({ client_version: "0.6.20" }, "0.6.20")).toBe(false);
    expect(clientIsStale({ client_version: "v0.6.20" }, "0.6.20")).toBe(false);
  });

  it("says nothing in a browser, where the page is the client", () => {
    expect(clientIsStale(null, "0.6.20")).toBe(false);
    expect(clientIsStale({}, "0.6.20")).toBe(false);
  });

  // A development build against a released server differs by definition, and
  // telling whoever is working on the thing to restart it every few minutes is
  // how a warning gets ignored for the one time it is real.
  it("says nothing about a development build", () => {
    expect(clientIsStale({ client_version: "dev" }, "0.6.20")).toBe(false);
    expect(clientIsStale({ client_version: "0.6.20" }, "dev")).toBe(false);
  });

  it("says nothing when the server has not answered yet", () => {
    expect(clientIsStale({ client_version: "0.6.19" }, undefined)).toBe(false);
  });
});
