import { Routes, Route } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import { Home } from "@/screens/Home";
import { Browse } from "@/screens/Browse";
import { Playlists } from "@/screens/Playlists";
import { Collections } from "@/screens/Collections";
import { Timeline } from "@/screens/Timeline";
import { FacePeople } from "@/screens/FacePeople";
import { PhotoSearch } from "@/screens/PhotoSearch";
import { Search } from "@/screens/Search";
import { Detail } from "@/screens/Detail";
import { Player } from "@/screens/Player";
import { Settings } from "@/screens/Settings";
import { Review } from "@/screens/Review";
import { Profile } from "@/screens/Profile";
import { Downloads } from "@/screens/Downloads";
import { Addons } from "@/screens/Addons";
import { LiveTV } from "@/screens/LiveTV";
import { People } from "@/screens/People";
import { Stub } from "@/screens/Stub";
import { Setup, Login } from "@/screens/Auth";
import { MiniPlayer } from "@/components/MiniPlayer";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
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
      <AppShell>
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/search" element={<Search />} />
          <Route path="/library/:id" element={<Browse />} />
          {/* A page *of* a library, not a global one: a playlist belongs to the
              library its tracks and its .m3u live in (ADR 0030). */}
          <Route path="/library/:id/playlists" element={<Playlists />} />
          <Route path="/library/:id/collections" element={<Collections />} />
          {/* A picture library by capture date, beside its folder grid. */}
          <Route path="/library/:id/timeline" element={<Timeline />} />
          {/* The people in a picture library — face groups, not accounts. */}
          <Route path="/library/:id/people" element={<FacePeople />} />
          <Route path="/library/:id/photos/search" element={<PhotoSearch />} />
          <Route path="/item/:id" element={<Detail />} />
          <Route path="/watch/:id" element={<Player />} />
          <Route path="/review" element={<Review />} />
          <Route path="/profile" element={<Profile />} />
          <Route path="/downloads" element={<Downloads />} />
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
      </AppShell>
    </PlaybackProvider>
  );
}
