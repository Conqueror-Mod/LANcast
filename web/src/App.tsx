import { Routes, Route } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import { Home } from "@/screens/Home";
import { Browse } from "@/screens/Browse";
import { Detail } from "@/screens/Detail";
import { Player } from "@/screens/Player";
import { Settings } from "@/screens/Settings";
import { Review } from "@/screens/Review";
import { Stub } from "@/screens/Stub";

export function App() {
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
