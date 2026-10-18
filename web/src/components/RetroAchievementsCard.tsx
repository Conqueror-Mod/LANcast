import { useState, type FormEvent } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiGet, apiPost, apiSend } from "@/api/client";
import type { components } from "@/api/schema";
import { errorMessage } from "@/lib/errors";
import "./RetroAchievementsCard.css";

/*
 * Your RetroAchievements, on your profile page (ADR 0090).
 *
 * Yours alone, and opt-in: until you connect, nothing contacts
 * RetroAchievements and this section is a short form saying what connecting
 * does. Pictures are LANcast's own URLs; the server fetched them, so this page
 * never contacts RetroAchievements either.
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

export function RetroAchievementsCard() {
  const qc = useQueryClient();
  const { data } = useRetroAchievements();
  const [username, setUsername] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [showAll, setShowAll] = useState(false);

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
  const refresh = useMutation({
    mutationFn: () => apiGet<RAView>("/api/profile/retroachievements?refresh=1"),
    onSuccess: (v) => qc.setQueryData(KEY, v),
  });

  if (!data || !data.available) return null;

  if (!data.connected) {
    const submit = (e: FormEvent) => {
      e.preventDefault();
      connect.mutate();
    };
    return (
      <section className="ra" aria-label="RetroAchievements">
        <span className="section-label">RetroAchievements</span>
        <p className="ra__intro">
          Connect your RetroAchievements account to see your points, recent unlocks and progress here. Only
          you see it. Your Web API key is on your RetroAchievements settings page; LANcast keeps it encrypted and
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
          <button className="ra__link" disabled={disconnect.isPending} onClick={() => disconnect.mutate()}>
            Disconnect
          </button>
        </div>
      </div>

      <div className="ra__who">
        {data.avatar && <img className="ra__avatar" src={data.avatar} alt="" />}
        <div>
          <div className="ra__name">{data.user}</div>
          <div className="ra__points">
            {data.points.toLocaleString()} points
            {data.softcore_points > 0 && <> · {data.softcore_points.toLocaleString()} casual</>}
          </div>
          {data.rich_presence && <div className="ra__presence">{data.rich_presence}</div>}
        </div>
      </div>

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
