import { describe, expect, it, vi } from "vitest";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { agentAsset, detail, me, page } from "../test/fixtures";

const compliance = {
  state: "noncompliant", noncompliant_since: "2026-11-15T10:00:00Z", evaluated_at: "2026-11-16T10:00:00Z", reported_at: "2026-11-16T10:00:00Z",
  up_to_date: true, server_reasons: ["Lleva 15 días sin conectar (máximo 14)."],
  results: [
    { policy_id: "p1", policy_name: "Seguridad Windows", key: "windows.firewall", title: "Cortafuegos activado", status: "noncompliant", message: "Desactivado en: Public.", applied: false, evaluated_at: "" },
    { policy_id: "p1", policy_name: "Seguridad Windows", key: "windows.bitlocker", title: "BitLocker obligatorio", status: "compliant", message: "Protegido.", applied: true, evaluated_at: "" },
  ],
  conflicts: [{ key: "windows.screen_lock", title: "Bloqueo de pantalla por inactividad", winner_id: "p1", winner_name: "Seguridad Windows", loser_id: "p2", loser_name: "Oficina" }],
};

function setup(role: "admin" | "operator" = "admin") {
  return mockApi({
    "GET /auth/me": { body: me(role) },
    "GET /groups": { body: [] },
    "GET /devices/d1": { body: detail({ compliance: "noncompliant" } as never) },
    "GET /devices/d1/compliance": { body: compliance },
    "GET /devices/d1/recovery-keys": { body: [{ key_id: "{K1}", volume: "C:", reported_at: "2026-11-16T09:00:00Z" }] },
    "POST /devices/d1/recovery-keys/{K1}/reveal": { body: { key_id: "{K1}", volume: "C:", recovery_password: "111111-222222-333333-444444-555555-666666-777777-888888" } },
    "GET /assets": () => ({ body: page([agentAsset({ compliance: "noncompliant" } as never)]) }),
    "GET /asset-categories": { body: [] },
  });
}

describe("Cumplimiento del dispositivo", () => {
  it("la pestaña muestra cada ajuste con su política y mensaje, y los conflictos", async () => {
    setup();
    renderApp("/dispositivos/d1");
    await userEvent.setup().click(await screen.findByRole("tab", { name: "Cumplimiento" }));
    const row = within((await screen.findByText("Cortafuegos activado")).closest("tr")!);
    expect(row.getByText("Seguridad Windows")).toBeInTheDocument();
    expect(row.getByText("Desactivado en: Public.")).toBeInTheDocument();
    expect(row.getByText("No cumple")).toBeInTheDocument();
    expect(within(screen.getByText("BitLocker obligatorio").closest("tr")!).getByText(/aplicado/i)).toBeInTheDocument();
    expect(screen.getByText("Lleva 15 días sin conectar (máximo 14).")).toBeInTheDocument();
    expect(screen.getByText(/Seguridad Windows.*gana sobre.*Oficina/)).toBeInTheDocument();
  });

  it("Mostrar clave pide confirmación, muestra la clave y la oculta a los 60 s", async () => {
    const calls = setup();
    renderApp("/dispositivos/d1");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("tab", { name: "Cumplimiento" }));
    await user.click(await screen.findByRole("button", { name: "Mostrar clave" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("quedará registrada");
    // A partir de aquí el tiempo es simulado, para adelantar el minuto.
    vi.useFakeTimers();
    within(screen.getByRole("dialog")).getByRole("button", { name: "Mostrar clave" }).click();
    for (let i = 0; i < 5 && !screen.queryByText(/111111-222222/); i++) {
      await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    }
    expect(screen.getByText("111111-222222-333333-444444-555555-666666-777777-888888")).toBeInTheDocument();
    expect(calls.some((c) => c.method === "POST" && c.path.includes("/reveal"))).toBe(true);
    for (let i = 0; i < 61; i++) {
      await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    }
    vi.useRealTimers();
    await waitFor(() => expect(screen.queryByText(/111111-222222/)).toBeNull());
  });

  it("un operador ve que hay clave custodiada pero no puede mostrarla", async () => {
    setup("operator");
    renderApp("/dispositivos/d1");
    await userEvent.setup().click(await screen.findByRole("tab", { name: "Cumplimiento" }));
    expect(await screen.findByText(/Clave de recuperación custodiada/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Mostrar clave" })).toBeNull();
  });

  it("el filtro de cumplimiento añade compliance= a la URL y a la petición", async () => {
    const calls = setup();
    const { location } = renderApp("/inventario?origin=agent");
    await screen.findByRole("link", { name: "PC-CONTA-07" });
    await userEvent.setup().selectOptions(screen.getByLabelText("Cumplimiento"), "noncompliant");
    await waitFor(() => expect(location()).toContain("compliance=noncompliant"));
    expect(calls.some((c) => c.path.includes("compliance=noncompliant"))).toBe(true);
  });
});
