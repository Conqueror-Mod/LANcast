/*
 * A person, drawn (ADR 0121): which of picture, animal and initials shows,
 * that every change refreshes every list a picture is drawn in, and that a
 * friend from a paired server is never mistaken for somebody here.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { PersonPicture } from "./Avatar";
import { useClearUserPicture, useCurrentUser, useUploadPicture } from "@/api/hooks";
import { Profile } from "@/screens/Profile";
import { FocusProvider } from "@/focus/FocusController";

vi.mock("@/playback/PlaybackProvider", () => ({
  usePlayback: () => ({ itemID: 1, displayTime: 0 }),
}));
vi.mock("@/playback/TogetherProvider", () => ({
  useTogetherRoom: () => ({
    isHost: true,
    session: {
      id: "ABC123",
      members: [
        { user_id: "u1", name: "Conqueror", host: true },
        // Same id as the signed-in account, but somebody else's server.
        { user_id: "u1", name: "Gerda", server: "Utopia" },
      ],
    },
    leave: () => undefined,
  }),
}));

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const HASH = "a".repeat(64);
let host: HTMLDivElement;
let root: Root;
let picture: string | null;
let calls: string[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  picture = HASH;
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push(`${method} ${url}`);
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), {
          status,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/profile/picture") && method === "DELETE") {
        picture = null;
        return new Response(null, { status: 204 });
      }
      if (url.includes("/api/profile/picture") && method === "PUT") {
        picture = HASH;
        return json({ picture: HASH });
      }
      if (url.includes("/api/users/") && method === "DELETE") {
        return new Response(null, { status: 204 });
      }
      if (url.includes("/api/auth/status")) {
        return json({
          configured: true,
          authenticated: true,
          user: { id: "u1", name: "Conqueror", role: "admin", avatar: "owl", picture },
        });
      }
      if (url.includes("/api/people")) {
        return json({ people: [] });
      }
      if (url.includes("/api/profile")) {
        return json({
          user: { name: "Conqueror", admin: true, secured: true },
          stats: { started: 0, finished: 0, watched_ms: 0, first_at: null },
          history: [],
          has_more: false,
        });
      }
      return json({});
    }),
  );
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

async function render(node: React.ReactNode, client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <FocusProvider>{node}</FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
  return client;
}

describe("PersonPicture", () => {
  it("draws the uploaded picture first, at the small size when drawn small", async () => {
    await render(<PersonPicture name="Ada Lovelace" picture={HASH} avatar="owl" size={38} />);
    const img = host.querySelector("img");
    expect(img?.getAttribute("src")).toBe(`/api/pictures/${HASH}?size=64`);
  });

  it("asks for the large size when drawn large", async () => {
    await render(<PersonPicture name="Ada" picture={HASH} size={96} />);
    expect(host.querySelector("img")?.getAttribute("src")).toBe(`/api/pictures/${HASH}?size=256`);
  });

  it("falls back to the animal, then to initials", async () => {
    await render(<PersonPicture name="Ada Lovelace" avatar="owl" size={38} />);
    expect(host.querySelector("svg")?.getAttribute("data-avatar")).toBe("owl");
    await render(<PersonPicture name="Ada Lovelace" size={38} />);
    expect(host.textContent).toBe("AL");
  });

  it("falls back when the picture fails to load, rather than drawing a broken image", async () => {
    await render(<PersonPicture name="Ada" picture={HASH} avatar="fox" size={38} />);
    await act(async () => {
      host.querySelector("img")!.dispatchEvent(new Event("error"));
    });
    expect(host.querySelector("img")).toBeNull();
    expect(host.querySelector("svg")?.getAttribute("data-avatar")).toBe("fox");
  });
});

describe("a picture change refreshes everywhere it is drawn", () => {
  function Mutations({ go }: { go: (m: { upload: ReturnType<typeof useUploadPicture>; clear: ReturnType<typeof useClearUserPicture> }) => void }) {
    const upload = useUploadPicture();
    const clear = useClearUserPicture();
    go({ upload, clear });
    return null;
  }

  for (const which of ["upload", "clear"] as const) {
    it(`${which} invalidates auth-status, people and users`, async () => {
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      const spy = vi.spyOn(client, "invalidateQueries");
      let m!: { upload: ReturnType<typeof useUploadPicture>; clear: ReturnType<typeof useClearUserPicture> };
      await render(<Mutations go={(x) => (m = x)} />, client);
      await act(async () => {
        if (which === "upload") // jsdom's Blob has no arrayBuffer; the hook needs nothing else of it.
          await m.upload.mutateAsync({ arrayBuffer: async () => new ArrayBuffer(1) } as unknown as Blob);
        else await m.clear.mutateAsync("u2");
      });
      const keys = spy.mock.calls.map((c) => JSON.stringify(c[0]?.queryKey));
      for (const k of ['["auth-status"]', '["people"]', '["users"]']) {
        expect(keys).toContain(k);
      }
    });
  }
});

describe("the profile", () => {
  function Rail() {
    const user = useCurrentUser();
    return user ? (
      <span className="rail-test">
        <PersonPicture name={user.name} picture={user.picture} avatar={user.avatar} size={18} fallback="glyph" />
      </span>
    ) : null;
  }

  it("removing the picture takes it off the rail too, back to the animal", async () => {
    await render(
      <>
        <Rail />
        <Profile />
      </>,
    );
    expect(host.querySelector(".rail-test img")).not.toBeNull();
    await act(async () => host.querySelector<HTMLButtonElement>(".profile__edit")!.click());
    const remove = [...document.querySelectorAll("button")].find((b) => b.textContent === "Remove your picture")!;
    await act(async () => remove.click());
    await settle();
    expect(calls).toContain("DELETE /api/profile/picture");
    expect(host.querySelector(".rail-test img")).toBeNull();
    expect(host.querySelector(".rail-test svg")?.getAttribute("data-avatar")).toBe("owl");
  });

  it("opens the editor in the page, with the file chooser's button focused", async () => {
    picture = null;
    await render(<Profile />);
    const open = host.querySelector<HTMLButtonElement>('.profile__edit[aria-label="Edit your picture"]')!;
    await act(async () => open.click());
    const dialog = document.querySelector('[role="dialog"][aria-label="Your picture"]');
    expect(dialog).not.toBeNull();
    expect(document.activeElement?.textContent).toBe("Choose a picture…");
    expect(document.querySelector('input[type="file"]')?.getAttribute("accept")).toBe("image/*");
  });
});

describe("a Watch Together room", () => {
  it("never gives a friend from another server a picture from here", async () => {
    const { TogetherPanel } = await import("./TogetherPanel");
    await render(<TogetherPanel onClose={() => undefined} />);
    const rows = [...host.querySelectorAll(".together__member")];
    expect(rows).toHaveLength(2);
    expect(rows[0].querySelector("img")?.getAttribute("src")).toBe(`/api/pictures/${HASH}?size=64`);
    expect(rows[1].querySelector("img")).toBeNull();
    expect(rows[1].textContent).toContain("GE");
  });
});

describe("the picture editor", () => {
  it("moves the picture with arrows, and keeps those keys from moving focus", async () => {
    // jsdom loads no images; this one reports its size as a real one would.
    vi.stubGlobal(
      "Image",
      class {
        naturalWidth = 400;
        naturalHeight = 200;
        onload: (() => void) | null = null;
        onerror: (() => void) | null = null;
        set src(_: string) {
          setTimeout(() => this.onload?.(), 0);
        }
      },
    );
    URL.createObjectURL = () => "blob:test";
    URL.revokeObjectURL = () => undefined;
    const { PictureEditor } = await import("./PictureEditor");
    await render(<PictureEditor onClose={() => undefined} />);

    const input = document.querySelector<HTMLInputElement>('input[type="file"]')!;
    Object.defineProperty(input, "files", { value: [new File(["x"], "me.jpg")] });
    await act(async () => input.dispatchEvent(new Event("change", { bubbles: true })));
    await settle();

    const frame = document.querySelector<HTMLElement>(".picdlg__frame")!;
    const img = frame.querySelector<HTMLImageElement>("img")!;
    // 400 × 200 in a 260 frame: centred, so the left edge starts at -130.
    expect(img.style.left).toBe("-130px");

    const leaked = vi.fn();
    document.addEventListener("keydown", leaked);
    await act(async () => {
      frame.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true }));
    });
    document.removeEventListener("keydown", leaked);
    expect(img.style.left).toBe("-118px");
    expect(leaked).not.toHaveBeenCalled();
  });
});
