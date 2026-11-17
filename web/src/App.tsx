import { Navigate, Route, Routes } from "react-router-dom";
import { AppProvider, useApp } from "./lib/store";
import Shell from "./components/Shell";
import Login from "./pages/Login";
import Overview from "./pages/Overview";
import Computers from "./pages/Computers";
import ComputerDetail from "./pages/ComputerDetail";
import Enrollments from "./pages/Enrollments";
import Alerts from "./pages/Alerts";
import Discovery from "./pages/Discovery";
import Reports from "./pages/Reports";
import Settings from "./pages/Settings";
import { Spinner } from "./components/ui";

function Routed() {
  const { me, loading } = useApp();
  if (loading && !me) {
    return (
      <div className="grid min-h-screen place-items-center">
        <Spinner label="Connecting to the server…" />
      </div>
    );
  }
  if (!me) return <Login />;
  return (
    <Routes>
      <Route element={<Shell />}>
        <Route path="/" element={<Overview />} />
        <Route path="/computers" element={<Computers />} />
        <Route path="/computers/:id" element={<ComputerDetail />} />
        <Route path="/enrollments" element={<Enrollments />} />
        <Route path="/alerts" element={<Alerts />} />
        <Route path="/discovery" element={<Discovery />} />
        <Route path="/reports" element={<Reports />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}

export default function App() {
  return (
    <AppProvider>
      <Routed />
    </AppProvider>
  );
}
