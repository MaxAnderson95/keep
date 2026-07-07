interface ConfirmSheetProps {
  title: string;
  message: string;
  confirmLabel: string;
  danger?: boolean;
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/** Bottom confirmation sheet — every verb confirms before firing (W3). */
export function ConfirmSheet({
  title,
  message,
  confirmLabel,
  danger,
  busy,
  onConfirm,
  onCancel,
}: ConfirmSheetProps) {
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center">
      <button
        type="button"
        aria-label="Cancel"
        className="absolute inset-0 bg-black/60"
        onClick={onCancel}
      />
      <div className="safe-bottom relative w-full max-w-lg rounded-t-2xl border-t border-edge bg-panel p-4 shadow-2xl">
        <h2 className="text-base font-semibold">{title}</h2>
        <p className="mt-1 whitespace-pre-line text-sm text-dim">{message}</p>
        <div className="mt-4 flex gap-2">
          <button
            type="button"
            className="flex-1 rounded-lg border border-edge px-4 py-2.5 text-sm font-medium"
            onClick={onCancel}
            disabled={busy}
          >
            Cancel
          </button>
          <button
            type="button"
            className={`flex-1 rounded-lg px-4 py-2.5 text-sm font-semibold text-white disabled:opacity-50 ${
              danger ? "bg-red-600" : "bg-emerald-600"
            }`}
            onClick={onConfirm}
            disabled={busy}
          >
            {busy ? "Working…" : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
