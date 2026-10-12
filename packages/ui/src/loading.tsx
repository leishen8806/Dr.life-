export function Loading({ label }: { label: string }) {
  return (
    <div className="drl-loading" role="status" aria-live="polite">
      <span className="drl-loading__dot" aria-hidden="true" />
      {label}
    </div>
  );
}
