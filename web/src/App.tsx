import { Routes, Route } from "react-router-dom";
import { PeerLibrary } from "@/screens/PeerLibrary";
import { PeerPlayer } from "@/screens/PeerPlayer";
import { AppShell } from "@/components/AppShell";
import { Home } from "@/screens/Home";
import { Browse } from "@/screens/Browse";
import { Playlists } from "@/screens/Playlists";
import { Collections } from "@/screens/Collections";
import { Timeline } from "@/screens/Timeline";
import { Duplicates } from "@/screens/Duplicates";
import { FacePeople } from "@/screens/FacePeople";
import { PhotoSearch } from "@/screens/PhotoSearch";
import { Search } from "@/screens/Search";
import { Detail } from "@/screens/Detail";
import { Player } from "@/screens/Player";
import { Settings } from "@/screens/Settings";
import { Review } from "@/screens/Review";
import { Profile } from "@/screens/Profile";
import { Favourites, TagItems } from "@/screens/Marked";
import { WatchHistory } from "@/screens/WatchHistory";
import { Downloads } from "@/screens/Downloads";
import { Games } from "@/screens/Games";
import { GameDetail } from "@/screens/GameDetail";
import { Addons } from "@/screens/Addons";
import { LiveTV } from "@/screens/LiveTV";
import { People } from "@/screens/People";
import { Stub } from "@/screens/Stub";
import { Setup, Login } from "@/screens/Auth";
import { MiniPlayer } from "@/components/MiniPlayer";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { TogetherProvider } from "@/playback/TogetherProvider";
import { JoinRequestPrompt } from "@/components/JoinRequestPrompt";
import { useAuthStatus } from "@/api/hooks";
import { DesignBench } from "@/screens/DesignBench";
import "@/playback/playback.css";

export function App() {
  const { data: auth, isLoading } = useAuthStatus();

  /*
   * The design bench sits in front of the gate, in dev builds only.
   *
   * The look is the one thing this project cannot review the way it reviews
   * everything else: jsdom paints no colour, and every screen that carries the
   * identity is behind a sign-in. Vite eliminates this branch in a production
   * build, so the page is absent from the shipped client rather than merely
   * unreachable inside it.
   */
  if (import.meta.env.DEV && window.location.pathname === "/design") {
    return <DesignBench />;
  }

  // Hold the gate until we know: flashing the library and then yanking it back
  // to a login screen is worse than a beat of nothing over the nebula field.
  if (isLoading || !auth) return null;

  // No account yet — first run. The server is loopback-only until one exists,
  // so this is only reachable from the machine itself.
  if (!auth.configured)
    return <Setup restartRequired={auth.restart_required} />;

  // Configured but not signed in.
  if (!auth.authenticated) return <Login />;

  // Playback wraps the router, not a route: the media element has to outlive
  // any single screen or leaving the player would stop the sound (ADR 0024's
  // client scope, docs/music-client-plan.md).
  return (
    <PlaybackProvider>
      <TogetherProvider>
      <AppShell>
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/search" element={<Search />} />
          <Route path="/library/:id" element={<Browse />} />
          {/*
            Somebody else's library, on its own path rather than /library/:id
            with a flag. The ids are not in the same namespace — item 42 on
            their server is a different film from item 42 here — so a shared
            route would be one mistyped parameter away from showing the wrong
            thing (ADR 0071 §5).
          */}
          <Route
            path="/peers/:fingerprint/library/:library"
            element={<PeerLibrary />}
          />
          {/* Watching one of theirs. Its own player for the same reason as the
              line above: the household's player carries a queue, a resume
              position and progress writes, none of which a peer item may
              touch (ADR 0071 §5). */}
          {/* Inside one of their shows, seasons, artists or albums. The same
              screen, listing one container's contents rather than the top. */}
          <Route
            path="/peers/:fingerprint/library/:library/in/:parent"
            element={<PeerLibrary />}
          />
          <Route
            path="/peers/:fingerprint/item/:item"
            element={<PeerPlayer />}
          />
          {/* A page *of* a library, not a global one: a playlist belongs to the
              library its tracks and its .m3u live in (ADR 0030). */}
          <Route path="/library/:id/playlists" element={<Playlists />} />
          <Route path="/library/:id/collections" element={<Collections />} />
          {/* A picture library by capture date, beside its folder grid. */}
          <Route path="/library/:id/timeline" element={<Timeline />} />
          <Route path="/library/:id/duplicates" element={<Duplicates />} />
          {/* The people in a picture library — face groups, not accounts. */}
          <Route path="/library/:id/people" element={<FacePeople />} />
          <Route path="/library/:id/photos/search" element={<PhotoSearch />} />
          <Route path="/item/:id" element={<Detail />} />
          <Route path="/watch/:id" element={<Player />} />
          <Route path="/review" element={<Review />} />
          <Route path="/profile" element={<Profile />} />
          <Route path="/profile/favourites" element={<Favourites />} />
          <Route path="/profile/tags/:id" element={<TagItems />} />
          <Route path="/profile/viewings" element={<WatchHistory />} />
          <Route path="/downloads" element={<Downloads />} />
          {/* The games on *this* machine (ADR 0066). Routed for everyone and
              useful to almost nobody: without the desktop bindings the page
              says so, which is a better answer than a 404 to somebody who
              followed a link from the machine where it works. */}
          <Route path="/games" element={<Games />} />
          <Route path="/games/:id" element={<GameDetail />} />
          <Route path="/addons" element={<Addons />} />
          <Route path="/live" element={<LiveTV />} />
          <Route path="/people" element={<People />} />
          <Route path="/settings" element={<Settings />} />
          <Route
            path="*"
            element={<Stub name="Not found" note="No such page." />}
          />
        </Routes>
        {/* Outside Routes: it is what you see *instead of* the player screen. */}
        <MiniPlayer />
        {/* A friend on another server asking to join what is playing. Outside
            Routes for the same reason as the mini player: it has to reach the
            host on whatever screen they are on. */}
        <JoinRequestPrompt />
      </AppShell>
      </TogetherProvider>
    </PlaybackProvider>
  );
}
