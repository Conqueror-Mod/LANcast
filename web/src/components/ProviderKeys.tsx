import { useState, type ReactNode } from "react";
import type { Settings, SettingsUpdate } from "@/api/types";

/*
 * Settings › Metadata: the provider keys (ADR 0110).
 *
 * A release carries its own TMDB and OpenSubtitles keys, so most people never
 * type one. Each row therefore says which key is *in use* — the person's, the
 * built-in one, or none — because "is this working" is what somebody looking
 * at the row is asking. OMDb has no built-in key (its free allowance is per
 * key and would be spent in hours if shared), so its row says where to get one.
 */

export const OMDB_KEY_URL = "https://www.omdbapi.com/apikey.aspx";

export function keyStatus(configured: boolean, source?: string): string {
  if (source === "own" || (source === undefined && configured)) return "Using your key";
  if (source === "built_in") return "Using LANcast's built-in key";
  return "Not set";
}

function ProviderKey({
  label,
  configured,
  source,
  onSave,
  pending,
  hint,
}: {
  label: string;
  configured: boolean;
  source?: string;
  onSave: (value: string) => void;
  pending: boolean;
  hint?: ReactNode;
}) {
  const [value, setValue] = useState("");
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{label}</div>
        <div className="set-row__sub">
          {hint && <>{hint} · </>}
          {keyStatus(configured, source)} · stored write-only
        </div>
      </div>
      <div className="set-row__actions">
        <input
          className="set-input"
          type="password"
          aria-label={`${label} API key`}
          placeholder={configured ? "Replace key" : source === "built_in" ? "Your own key" : "Enter key"}
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
        <button
          className="set-btn"
          disabled={!value || pending}
          onClick={() => {
            onSave(value);
            setValue("");
          }}
        >
          Save
        </button>
        {/* Removing your own key is how you go back to the built-in one. */}
        {configured && (
          <button className="set-btn" disabled={pending} onClick={() => onSave("")}>
            Remove
          </button>
        )}
      </div>
    </div>
  );
}

function OpenSubtitlesLogin({
  username,
  pending,
  save,
}: {
  username: string;
  pending: boolean;
  save: (patch: SettingsUpdate) => void;
}) {
  const [user, setUser] = useState("");
  const [password, setPassword] = useState("");
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">OpenSubtitles account</div>
        <div className="set-row__sub">
          {username ? (
            <>Downloads use {username}'s daily allowance · password stored write-only</>
          ) : (
            <>Optional. Signing in with a free opensubtitles.com account raises how many subtitles you can download a day.</>
          )}
        </div>
      </div>
      <div className="set-row__actions">
        {username ? (
          <button
            className="set-btn"
            disabled={pending}
            onClick={() => save({ opensubtitles_username: "" })}
          >
            Remove
          </button>
        ) : (
          <>
            <input
              className="set-input"
              aria-label="OpenSubtitles username"
              placeholder="Username"
              autoComplete="off"
              value={user}
              onChange={(e) => setUser(e.target.value)}
            />
            <input
              className="set-input"
              type="password"
              aria-label="OpenSubtitles password"
              placeholder="Password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <button
              className="set-btn"
              disabled={!user.trim() || !password || pending}
              onClick={() => {
                save({ opensubtitles_username: user.trim(), opensubtitles_password: password });
                setUser("");
                setPassword("");
              }}
            >
              Save
            </button>
          </>
        )}
      </div>
    </div>
  );
}

export function ProviderKeys({
  settings,
  pending,
  save,
}: {
  settings: Settings;
  pending: boolean;
  save: (patch: SettingsUpdate) => void;
}) {
  const anyBuiltIn = settings.tmdb.built_in || settings.opensubtitles.built_in;
  return (
    <>
      <ProviderKey
        label="TMDB"
        hint="Film and show details, posters and artwork"
        configured={settings.tmdb.configured}
        source={settings.tmdb.source}
        pending={pending}
        onSave={(v) => save({ tmdb_key: v })}
      />
      <ProviderKey
        label="OpenSubtitles"
        hint="Subtitle search"
        configured={settings.opensubtitles.configured}
        source={settings.opensubtitles.source}
        pending={pending}
        onSave={(v) => save({ opensubtitles_key: v })}
      />
      {settings.opensubtitles.source !== "none" && (
        <OpenSubtitlesLogin
          username={settings.opensubtitles.username}
          pending={pending}
          save={save}
        />
      )}
      <ProviderKey
        label="OMDb"
        hint={
          <>
            Optional: Rotten Tomatoes, Metacritic and IMDb ratings. Get a free key at{" "}
            <a href={OMDB_KEY_URL} target="_blank" rel="noreferrer noopener">
              omdbapi.com
            </a>
          </>
        }
        configured={settings.omdb.configured}
        pending={pending}
        onSave={(v) => save({ omdb_key: v })}
      />

      {/*
        Only offered by a build that carries a key: in one that does not, the
        switch would change nothing, and a control that does nothing reads as
        broken.
      */}
      {anyBuiltIn && (
        <>
          <label className="set-toggle">
            <input
              type="checkbox"
              checked={!settings.builtin_keys_off}
              disabled={pending}
              onChange={(e) => save({ builtin_keys_off: !e.target.checked })}
            />
            Use LANcast's built-in keys
          </label>
          <div className="set-row__sub set-row__sub--standalone">
            This release comes with its own TMDB and OpenSubtitles keys, so details and
            subtitles work without signing up anywhere. A key you enter above is always used
            instead. Turn this off and a service you have not given a key to is never
            contacted.
          </div>
        </>
      )}
    </>
  );
}
