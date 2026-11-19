import type { ReactNode } from "react";

export default function EmptyState({ message, children }: { message: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <p>{message}</p>
      {children && <p>{children}</p>}
    </div>
  );
}
