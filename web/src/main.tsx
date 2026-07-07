import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { AuthProvider, useAuth } from "./auth";
import { Layout } from "./components/Layout";
import { DiffPage } from "./pages/Diff";
import { DoctorPage } from "./pages/Doctor";
import { LoginPage } from "./pages/Login";
import { ServiceDetailPage } from "./pages/ServiceDetail";
import { ServicesPage } from "./pages/Services";
import { SettingsPage } from "./pages/Settings";
import "./index.css";

function App() {
  const { phase } = useAuth();
  if (phase === "loading") {
    return (
      <div className="flex min-h-dvh items-center justify-center">
        <p className="text-sm text-dim">keep</p>
      </div>
    );
  }
  if (phase === "anon") {
    return <LoginPage />;
  }
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<ServicesPage />} />
        <Route path="/services/:name" element={<ServiceDetailPage />} />
        <Route path="/diff" element={<DiffPage />} />
        <Route path="/doctor" element={<DoctorPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <AuthProvider>
        <App />
      </AuthProvider>
    </BrowserRouter>
  </StrictMode>,
);
