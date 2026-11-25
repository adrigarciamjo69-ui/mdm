import { describe, expect, it } from "vitest";
import { activeTempAdmins } from "./tempAdmin";
import type { CommandRow } from "./commands";

const row = (id: string, type: string, status: string, finished: string | null, params: Record<string, unknown> = {}, created = finished ?? "2026-11-25T09:00:00Z"): CommandRow =>
  ({ id, device_id: "d1", hostname: "pc", type, params, status, exit_code: null, error: "", created_at: created, finished_at: finished });

describe("activeTempAdmins", () => {
  const now = new Date("2026-11-25T10:30:00Z");
  it("devuelve los vigentes y descarta caducados, fallidos y retirados", () => {
    const rows = [
      row("1", "temp_admin", "succeeded", "2026-11-25T10:00:00Z", { user: "EMPRESA\\ana", minutes: 60, reason: "Driver" }),
      row("2", "temp_admin", "succeeded", "2026-11-25T08:00:00Z", { user: "luis", minutes: 60 }),
      row("3", "temp_admin", "failed", "2026-11-25T10:00:00Z", { user: "jefe", minutes: 60 }),
      row("4", "temp_admin", "succeeded", "2026-11-25T10:10:00Z", { minutes: 30 }),
    ];
    const got = activeTempAdmins(rows, now);
    expect(got.map((g) => g.user)).toEqual(["EMPRESA\\ana", ""]);
    expect(got[0].expiresAt.toISOString()).toBe("2026-11-25T11:00:00.000Z");
  });
  it("una retirada posterior los quita (del usuario o de todos)", () => {
    const rows = [
      row("1", "temp_admin", "succeeded", "2026-11-25T10:00:00Z", { user: "ana", minutes: 60 }),
      row("2", "temp_admin", "succeeded", "2026-11-25T10:00:00Z", { user: "luis", minutes: 60 }),
      row("3", "temp_admin_revoke", "succeeded", "2026-11-25T10:05:00Z", { user: "ANA" }),
    ];
    expect(activeTempAdmins(rows, now).map((g) => g.user)).toEqual(["luis"]);
    rows.push(row("4", "temp_admin_revoke", "succeeded", "2026-11-25T10:06:00Z", { user: "" }));
    expect(activeTempAdmins(rows, now)).toEqual([]);
  });
});
