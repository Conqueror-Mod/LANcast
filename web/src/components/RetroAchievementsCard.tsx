import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiGet, apiPost, apiSend } from "@/api/client";
import type { components } from "@/api/schema";
import { errorMessage } from "@/lib/errors";
import "./RetroAchievementsCard.css";

/*
 * Your RetroAchievements (ADR 0090), in two places since settings-3-plan
 * step 4.
 *
 * The connection — connecting, disconnecting, earning while playing — is in
 * Settings › Your account › Connected accounts, where somebody looking for
 * "achievements" in Settings can find it (ADR 0112 step 6 asked for that). The
 * points, unlocks and progress stay on your profile, which is where you look
 * at what you have done.
 *
 * Yours alone, and opt-in: until you connect, nothing contacts
 * RetroAchievements. Pictures are LANcast's own URLs; the server fetched
 * them, so neither page contacts RetroAchievements either.
 */

export type RAView = components["schemas"]["RetroAchievementsView"];

const KEY = ["retroachievements"] as const;

export function useRetroAchievements() {
  return useQuery({
    queryKey: KEY,
    queryFn: ({ signal }) => apiGet<RAView>("/api/profile/retroachievements", signal),
    staleTime: 5 * 60_000,
  });
}

/** Connecting, disconnecting and earning while playing: Settings › Connected accounts. */
export function RetroAchievementsConnection() {
  const qc = useQueryClient();
  const { data, isPending } = useRetroAchievements();
  const [username, setUsername] = useState("");
  const [apiKey, setApiKey] = useState("");

  const connect = useMutation({
    mutationFn: () =>
      apiPost<RAView>("/api/profile/retroachievements", { username: username.trim(), api_key: apiKey.trim() }, "PUT"),
    onSuccess: (v) => {
      qc.setQueryData(KEY, v);
      setApiKey("");
    },
  });
  const disconnect = useMutation({
    mutationFn: () => apiSend("/api/profile/retroachievements", "DELETE"),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  });

  if (isPending) return null;
  if (!data || !data.available) {
    return (
      <section className="ra" aria-label="RetroAchievements">
        <span className="section-label">RetroAchievements</span>
        <p className="ra__intro">This server cannot reach RetroAchievements.</p>
      </section>
    );
  }

  if (!data.connected) {
    const submit = (e: FormEvent) => {
      e.preventDefault();
      connect.mutate();
    };
    return (
      <section className="ra" aria-label="RetroAchievements">
        <span className="section-label">RetroAchievements</span>
        <p className="ra__intro">
          Connect your RetroAchievements account to see your points, recent unlocks and progress on your
          profile. Only you see it. Your Web API key is on your RetroAchievements settings page; LANcast keeps it encrypted and
          contacts RetroAchievements only for you.
        </p>
        <form className="ra__form" onSubmit={submit}>
          <label className="ra__field">
            <span>Username</span>
            <input value={username} autoComplete="off" onChange={(e) => setUsername(e.target.value)} />
          </label>
          <label className="ra__field">
            <span>Web API key</span>
            <input
              type="password"
              value={apiKey}
              autoComplete="off"
              onChange={(e) => setApiKey(e.target.value)}
            />
          </label>
          <button className="ra__button" disabled={connect.isPending || !username.trim() || !apiKey.trim()}>
            {connect.isPending ? "Checking…" : "Connect"}
          </button>
        </form>
        {connect.isError && (
          <p className="ra__problem" role="alert">
            {errorMessage(connect.error)}
          </p>
        )}
      </section>
    );
  }

  return (
    <section className="ra" aria-label="RetroAchievements">
      <div className="ra__head">
        <span className="section-label">RetroAchievements</span>
        <div className="ra__actions">
          <button className="ra__link" disabled={disconnect.isPending} onClick={() => disconnect.mutate()}>
            Disconnect
          </button>
        </div>
      </div>
      <Who view={data} />
      {data.problem && (
        <p className="ra__problem" role="status">
          {data.problem}
        </p>
      )}
      <EarnWhilePlaying view={data} />
      <p className="ra__intro">
        Your points, recent unlocks and progress are on <Link to="/profile">your profile</Link>.
      </p>
    </section>
  );
}

/** Points, unlocks and progress: your profile. */
export function RetroAchievementsCard() {
  const qc = useQueryClient();
  const { data } = useRetroAchievements();
  const [showAll, setShowAll] = useState(false);
  const refresh = useMutation({
    mutationFn: () => apiGet<RAView>("/api/profile/retroachievements?refresh=1"),
    onSuccess: (v) => qc.setQueryData(KEY, v),
  });

  if (!data || !data.available) return null;

  if (!data.connected) {
    return (
      <section className="ra" aria-label="RetroAchievements">
        <span className="section-label">RetroAchievements</span>
        <p className="ra__intro">
          Connect your RetroAchievements account in{" "}
          <Link to="/settings?pane=accounts">Settings › Connected accounts</Link> to see your points, recent
          unlocks and progress here.
        </p>
      </section>
    );
  }

  const games = Array.isArray(data.games) ? data.games : [];
  const recent = Array.isArray(data.recent) ? data.recent : [];
  const shownGames = showAll ? games : games.slice(0, 8);

  return (
    <section className="ra" aria-label="RetroAchievements">
      <div className="ra__head">
        <span className="section-label">RetroAchievements</span>
        <div className="ra__actions">
          <button className="ra__link" disabled={refresh.isPending} onClick={() => refresh.mutate()}>
            {refresh.isPending ? "Refreshing…" : "Refresh"}
          </button>
          <Link className="ra__link" to="/settings?pane=accounts">
            Manage
          </Link>
        </div>
      </div>

      <Who view={data} />

      {data.problem && (
        <p className="ra__problem" role="status">
          {data.problem}
        </p>
      )}

      {recent.length > 0 && (
        <>
          <span className="ra__sub">Unlocked in the last two weeks</span>
          <ul className="ra__unlocks">
            {recent.slice(0, 12).map((u) => (
              <li key={`${u.achievement_id}-${u.date}`} className="ra__unlock">
                {u.badge ? <img className="ra__badge" src={u.badge} alt="" /> : <span className="ra__badge" />}
                <div className="ra__what">
                  <span className="ra__title">
                    {u.title}
                    {u.hardcore && <span className="ra__tag">hardcore</span>}
                  </span>
                  <span className="ra__detail">
                    {u.game_title} · {u.console} · {u.points} pts
                  </span>
                </div>
                <span className="ra__when">{shortDate(u.date)}</span>
              </li>
            ))}
          </ul>
        </>
      )}

      {games.length > 0 && (
        <>
          <span className="ra__sub">Progress</span>
          <ul className="ra__games">
            {shownGames.map((g) => {
              const pct = g.max > 0 ? Math.round((g.awarded / g.max) * 100) : 0;
              return (
                <li key={g.game_id} className="ra__game">
                  {g.icon ? <img className="ra__badge" src={g.icon} alt="" /> : <span className="ra__badge" />}
                  <div className="ra__what">
                    <span className="ra__title">
                      {g.title}
                      {g.award && <span className="ra__tag">{awardLabel(g.award)}</span>}
                    </span>
                    <span className="ra__detail">
                      {g.console} · {g.awarded} of {g.max}
                    </span>
                    <span className="ra__bar" aria-hidden="true">
                      <span style={{ width: `${pct}%` }} />
                    </span>
                  </div>
                  <span className="ra__when">{pct}%</span>
                </li>
              );
            })}
          </ul>
          {games.length > shownGames.length && (
            <button className="ra__link" onClick={() => setShowAll(true)}>
              Show all {games.length} games
            </button>
          )}
        </>
      )}

      {recent.length === 0 && games.length === 0 && !data.problem && (
        <p className="ra__intro">Nothing unlocked yet. Progress shows here as you play.</p>
      )}
    </section>
  );
}

/** Who is connected, and their points. */
function Who({ view }: { view: RAView }) {
  return (
    <div className="ra__who">
      {view.avatar && <img className="ra__avatar" src={view.avatar} alt="" />}
      <div>
        <div className="ra__name">{view.user}</div>
        <div className="ra__points">
          {view.points.toLocaleString()} points
          {view.softcore_points > 0 && <> · {view.softcore_points.toLocaleString()} casual</>}
        </div>
        {view.rich_presence && <div className="ra__presence">{view.rich_presence}</div>}
      </div>
    </div>
  );
}

// "2026-10-17 21:00:00" as a local date. RetroAchievements answers in UTC.
function shortDate(s: string): string {
  const d = new Date(s.replace(" ", "T") + "Z");
  return Number.isNaN(d.getTime()) ? s : d.toLocaleDateString();
}

function awardLabel(kind: string): string {
  switch (kind) {
    case "mastered":
      return "mastered";
    case "completed":
      return "completed";
    case "beaten-hardcore":
      return "beaten";
    case "beaten-softcore":
      return "beaten (casual)";
    default:
      return kind.replace(/-/g, " ");
  }
}

/*
 * Earning while playing (ADR 0112): the desktop player unlocks achievements
 * as you play, softcore only. Turning it on asks for the RetroAchievements
 * password once, which the server trades for a login token and does not
 * keep. The Web API key above only reads; it cannot unlock anything.
 */
function EarnWhilePlaying({ view }: { view: RAView }) {
  const qc = useQueryClient();
  const [asking, setAsking] = useState(false);
  const [password, setPassword] = useState("");
  const on = useMutation({
    mutationFn: () => apiPost<RAView>("/api/profile/retroachievements/play", { password }, "PUT"),
    onSuccess: (v) => {
      qc.setQueryData(KEY, v);
      setPassword("");
      setAsking(false);
    },
  });
  const off = useMutation({
    mutationFn: () => apiSend("/api/profile/retroachievements/play", "DELETE"),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  });

  if (view.earning) {
    return (
      <div className="ra__earn">
        <span className="ra__sub">Earning while playing</span>
        <p className="ra__intro">
          On. Games in the LANcast desktop app unlock achievements as you play, in casual (softcore) mode.
          {view.unlocks_waiting > 0 &&
            ` ${view.unlocks_waiting} ${view.unlocks_waiting === 1 ? "unlock is" : "unlocks are"} waiting to reach RetroAchievements.`}
        </p>
        <p className="ra__intro">
          Hardcore isn't offered yet. RetroAchievements counts hardcore unlocks only from emulators it has approved, and
          it hasn't approved LANcast.
        </p>
        <button className="ra__link" disabled={off.isPending} onClick={() => off.mutate()}>
          Turn off
        </button>
        {off.isError && (
          <p className="ra__problem" role="alert">
            {errorMessage(off.error)}
          </p>
        )}
      </div>
    );
  }

  const submit = (e: FormEvent) => {
    e.preventDefault();
    on.mutate();
  };
  return (
    <div className="ra__earn">
      <span className="ra__sub">Earning while playing</span>
      <p className="ra__intro">
        Unlock achievements as you play in the LANcast desktop app, in casual (softcore) mode, with save states
        and rewind still available. This needs your RetroAchievements password once; the server swaps it for a
        sign-in token and does not keep it. While you play, RetroAchievements also sees what you are doing in the
        game (its rich presence, such as "Level 3, 2 lives"); that can't be turned off on its own.
      </p>
      {!asking ? (
        <button className="ra__button" onClick={() => setAsking(true)}>
          Turn on
        </button>
      ) : (
        <form className="ra__form" onSubmit={submit}>
          <label className="ra__field">
            <span>RetroAchievements password for {view.user}</span>
            <input
              type="password"
              value={password}
              autoComplete="off"
              autoFocus
              onChange={(e) => setPassword(e.target.value)}
            />
          </label>
          <button className="ra__button" disabled={on.isPending || !password}>
            {on.isPending ? "Checking…" : "Turn on"}
          </button>
        </form>
      )}
      {on.isError && (
        <p className="ra__problem" role="alert">
          {errorMessage(on.error)}
        </p>
      )}
    </div>
  );
}
