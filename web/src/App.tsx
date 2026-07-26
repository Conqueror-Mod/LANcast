import { Routes, Route } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import { Home } from "@/screens/Home";
import { Browse } from "@/screens/Browse";
import { Detail } from "@/screens/Detail";
import { Stub } from "@/screens/Stub";

export function App() {
  return (
    <AppShell>
      <Routes>
        <Route path="/" element={<Home />} />
        <Route path="/library/:id" element={<Browse />} />
        <Route path="/item/:id" element={<Detail />} />
        <Route
          path="/watch/:id"
          element={<Stub name="Player" note="The chrome-free player is a later slice." />}
        />
        <Route
          path="/settings"
          element={
            <Stub
              name="Settings"
              note="Libraries with live scan progress, playback preferences, and the keyboard reference will live here."
            />
          }
        />
        <Route path="*" element={<Stub name="Not found" note="No such page." />} />
      </Routes>
    </AppShell>
  );
}
