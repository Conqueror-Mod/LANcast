import { useState } from "react";
import {
  useChannelSources,
  useAddChannelSource,
  useRefreshChannelSource,
  useDeleteChannelSource,
  useSetGuideURL,
} from "@/api/hooks";

/*
 * Live TV channel sources.
 *
 * Admin-gated for a stronger reason than most panes here: adding a source makes
 * *the server* fetch a URL somebody typed, which is server-side request forgery
 * in miniature. The server refuses its own address; everything else on the
 * network is allowed, because a tuner on the same machine is the ordinary case.
 */
export function LiveTVSection() {
  const { data } = useChannelSources(true);
  const add = useAddChannelSource();
  const refresh = useRefreshChannelSource();
  const remove = useDeleteChannelSource();
  const setGuide = useSetGuideURL();
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  // Two guide-URL fields, two states. Sharing one was the first shape and it
  // leaked: opening a source's guide editor prefilled the *add* form with that
  // source's URL, so the next list added inherited somebody else's guide.
  const [epgUrl, setEpgUrl] = useState("");
  const [newEpgUrl, setNewEpgUrl] = useState("");
  const [note, setNote] = useState<string | null>(null);
  // Which source's guide field is open. One at a time: this is a rarely-typed
  // URL, and a row of permanently-visible inputs makes the common case (look at
  // the list, refresh one) noisier for the sake of the rare one.
  const [editing, setEditing] = useState<number | null>(null);

  const sources = data?.sources ?? [];

  return (
    <section className="settings__section">
      <span className="section-label">Live TV</span>
      <p className="set-row__note">
        A channel list is an M3U — from an IPTV provider, or from a tuner on
        this network. Channels are played through this server, so the list URL
        and anything in it stays here.
      </p>
      <p className="set-row__note">
        A guide is a separate XMLTV file, plain or gzipped. Listings attach to
        channels by <code>tvg-id</code>, so a channel list that does not carry
        one gets no guide — there is no reliable way to match by name. Guides
        refresh by themselves every twelve hours.
      </p>

      {sources.map((src) => (
        <div className="set-lib" key={src.id}>
          <div className="set-row">
            <div className="set-row__main">
              <div className="set-row__title">{src.name}</div>
              <div className="set-row__sub">
                {src.channel_count.toLocaleString()} channels
                {src.refreshed_at
                  ? ` · refreshed ${new Date(src.refreshed_at * 1000).toLocaleDateString()}`
                  : " · never refreshed"}
                {/* A guide is either configured and working, configured and
                    empty, or absent — and those want three different next
                    actions, so they are three different sentences. */}
                {src.epg_url
                  ? ` · ${src.program_count.toLocaleString()} listings`
                  : " · no guide"}
              </div>
            </div>
            <div className="set-row__actions">
              <button
                className="set-btn"
                onClick={() => {
                  setEditing(editing === src.id ? null : src.id);
                  setEpgUrl(src.epg_url ?? "");
                }}
              >
                Guide
              </button>
              <button
                className="set-btn"
                disabled={refresh.isPending}
                onClick={() => refresh.mutate(src.id)}
              >
                {refresh.isPending ? "Refreshing…" : "Refresh"}
              </button>
              <button
                className="set-btn set-btn--danger"
                disabled={remove.isPending}
                onClick={() => remove.mutate(src.id)}
              >
                Remove
              </button>
            </div>
          </div>

          {editing === src.id && (
            <form
              className="set-add"
              onSubmit={(e) => {
                e.preventDefault();
                setNote(null);
                setGuide.mutate(
                  { id: src.id, epg_url: epgUrl.trim() },
                  {
                    onSuccess: (res) => {
                      setEditing(null);
                      setNote(
                        res.epg_error
                          ? `The guide could not be read: ${res.epg_error}`
                          : epgUrl.trim() === ""
                            ? "Guide removed."
                            : `Imported ${res.programs ?? 0} listings.`,
                      );
                    },
                  },
                );
              }}
            >
              <input
                className="set-input"
                placeholder="https://…/guide.xml or .xml.gz"
                value={epgUrl}
                onChange={(e) => setEpgUrl(e.target.value)}
                aria-label="XMLTV guide URL"
              />
              <button className="set-btn" disabled={setGuide.isPending}>
                {setGuide.isPending ? "Importing…" : "Save guide"}
              </button>
            </form>
          )}
        </div>
      ))}

      <form
        className="set-add"
        onSubmit={(e) => {
          e.preventDefault();
          setNote(null);
          add.mutate(
            { name: name.trim(), url: url.trim(), epg_url: newEpgUrl.trim() },
            {
              onSuccess: (res) => {
                setName("");
                setUrl("");
                setNewEpgUrl("");
                // A source whose import failed is kept, because the URL may be
                // right and the provider down. Saying which it was is the whole
                // value of the message.
                setNote(
                  res.import_error
                    ? `Added, but the list could not be read: ${res.import_error}`
                    : res.epg_error
                      ? `Added ${res.channels ?? 0} channels, but the guide could not be read: ${res.epg_error}`
                      : `Added ${res.channels ?? 0} channels` +
                        (res.programs ? ` and ${res.programs} listings.` : "."),
                );
              },
            },
          );
        }}
      >
        <input
          className="set-input"
          placeholder="Name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
        />
        <input
          className="set-input"
          placeholder="https://…/playlist.m3u"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          required
        />
        <button className="set-btn" disabled={add.isPending}>
          {add.isPending ? "Importing…" : "Add channel list"}
        </button>
      </form>
      {note && <span className="set-note">{note}</span>}
      {add.isError && (
        <span className="set-error">{(add.error as Error).message}</span>
      )}
    </section>
  );
}
