import type { ComplianceState, SettingStatus } from "../api/types";

const LABEL: Record<ComplianceState | SettingStatus, string> = {
  compliant: "Cumple",
  noncompliant: "No cumple",
  unknown: "Desconocido",
  error: "Error",
  not_applicable: "No aplicable",
};

const CLASS: Record<ComplianceState | SettingStatus, string> = {
  compliant: "status status-online",
  noncompliant: "status status-revoked",
  error: "status status-revoked",
  unknown: "status status-inactive",
  not_applicable: "status status-inactive",
};

export default function ComplianceBadge({ state }: { state: ComplianceState | SettingStatus | undefined }) {
  const s = state ?? "unknown";
  return <span className={CLASS[s]}>{LABEL[s]}</span>;
}
