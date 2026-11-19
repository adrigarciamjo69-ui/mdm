import type { Status } from "../api/types";
import { statusLabel } from "../lib/format";

export default function StatusBadge({ status }: { status: Status }) {
  return <span className={`status status-${status}`}>{statusLabel(status)}</span>;
}
