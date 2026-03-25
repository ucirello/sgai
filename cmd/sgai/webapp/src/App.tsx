import { Outlet } from "react-router";
import { ConnectionStatusBanner } from "./components/ConnectionStatusBanner";
import { NotificationPermissionBar } from "./components/NotificationPermissionBar";
import { TooltipProvider } from "./components/ui/tooltip";
import { useNotifications } from "./hooks/useNotifications";

export function App() {
  useNotifications();

  return (
    <TooltipProvider>
      <NotificationPermissionBar />
      <ConnectionStatusBanner />
      <div className="min-h-screen bg-background text-foreground">
        <main className="p-4">
          <Outlet />
        </main>
      </div>
    </TooltipProvider>
  );
}
