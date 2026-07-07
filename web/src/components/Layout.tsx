import { NavLink, Outlet } from "react-router-dom";

const TABS = [
  { to: "/", label: "Services", icon: ServicesIcon },
  { to: "/diff", label: "Diff", icon: DiffIcon },
  { to: "/doctor", label: "Doctor", icon: DoctorIcon },
  { to: "/settings", label: "Settings", icon: SettingsIcon },
];

export function Layout() {
  return (
    <div className="mx-auto flex min-h-dvh max-w-lg flex-col">
      <header className="safe-top sticky top-0 z-40 border-b border-edge bg-bg/90 backdrop-blur">
        <div className="flex items-center gap-2 px-4 py-3">
          <span className="inline-block h-2.5 w-2.5 rounded-full bg-emerald-400" />
          <h1 className="text-lg font-bold tracking-tight">keep</h1>
        </div>
      </header>
      <main className="flex-1 pb-24">
        <Outlet />
      </main>
      <nav className="safe-bottom fixed inset-x-0 bottom-0 z-40 border-t border-edge bg-panel/95 backdrop-blur">
        <div className="mx-auto flex max-w-lg">
          {TABS.map((tab) => (
            <NavLink
              key={tab.to}
              to={tab.to}
              end={tab.to === "/"}
              className={({ isActive }) =>
                `flex flex-1 flex-col items-center gap-0.5 py-2 text-[11px] font-medium ${
                  isActive ? "text-emerald-400" : "text-dim"
                }`
              }
            >
              <tab.icon />
              {tab.label}
            </NavLink>
          ))}
        </div>
      </nav>
    </div>
  );
}

function ServicesIcon() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <rect x="3" y="4" width="18" height="4.5" rx="1.5" stroke="currentColor" strokeWidth="1.8" />
      <rect
        x="3"
        y="10.5"
        width="18"
        height="4.5"
        rx="1.5"
        stroke="currentColor"
        strokeWidth="1.8"
      />
      <rect x="3" y="17" width="18" height="4.5" rx="1.5" stroke="currentColor" strokeWidth="1.8" />
    </svg>
  );
}

function DiffIcon() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M8 4v16M8 4l-3 3M8 4l3 3"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M16 20V4M16 20l-3-3M16 20l3-3"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function DoctorIcon() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M12 21s-7-4.5-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 11c0 5.5-7 10-7 10z"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
      <path
        d="M7.5 12h2l1.5-3 2 5 1.5-2h2"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function SettingsIcon() {
  return (
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="3" stroke="currentColor" strokeWidth="1.8" />
      <path
        d="M12 3v2.5M12 18.5V21M21 12h-2.5M5.5 12H3m14.8-6.8-1.8 1.8M8 16l-1.8 1.8m0-11.6L8 8m8 8 1.8 1.8"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
      />
    </svg>
  );
}
