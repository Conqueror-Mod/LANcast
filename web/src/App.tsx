import { Routes, Route } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import { Home } from "@/screens/Home";
import { Browse } from "@/screens/Browse";
import { Playlists } from "@/screens/Playlists";
import { Collections } from "@/screens/Collections";
import { Search } from "@/screens/Search";
import { Detail } from "@/screens/Detail";
import { Player } from "@/screens/Player";
import { Settings } from "@/screens/Settings";
import { Review } from "@/screens/Review";
import { Profile } from "@/screens/Profile";
import { Downloads } from "@/screens/Downloads";
import { Addons } from "@/screens/Addons";
import { Stub } from "@/screens/Stub";
import { Setup, Login } from "@/screens/Auth";
import { MiniPlayer } from "@/components/MiniPlayer";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { useAuthStatus } from "@/api/hooks";
import "@/playback/playback.css";

export function App() {
  const { data: auth, isLoading } = useAuthStatus();

  // Hold the gate until we know: flashing the library and then yanking it back
  // to a login screen is worse than a beat of nothing over the nebula field.
  if (isLoading || !auth) return null;

  // No account yet — first run. The server is loopback-only until one exists,
  // so this is only reachable from the machine itself.
  if (!auth.configured) return <Setup restartRequired={auth.restart_required} />;

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
          <Route path="/item/:id" element={<Detail />} />
          <Route path="/watch/:id" element={<Player />} />
          <Route path="/review" element={<Review />} />
          <Route path="/profile" element={<Profile />} />
          <Route path="/downloads" element={<Downloads />} />
          <Route path="/addons" element={<Addons />} />
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
