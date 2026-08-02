import { Routes, Route } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import { Home } from "@/screens/Home";
import { Browse } from "@/screens/Browse";
import { Detail } from "@/screens/Detail";
import { Player } from "@/screens/Player";
import { Settings } from "@/screens/Settings";
import { Review } from "@/screens/Review";
import { Stub } from "@/screens/Stub";
import { Setup, Login } from "@/screens/Auth";
import { useAuthStatus } from "@/api/hooks";

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

  return (
    <AppShell>
      <Routes>
        <Route path="/" element={<Home />} />
        <Route path="/library/:id" element={<Browse />} />
        <Route path="/item/:id" element={<Detail />} />
        <Route path="/watch/:id" element={<Player />} />
        <Route path="/review" element={<Review />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Stub name="Not found" note="No such page." />} />
      </Routes>
    </AppShell>
  );
}
