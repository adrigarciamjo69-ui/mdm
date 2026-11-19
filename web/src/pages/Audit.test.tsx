import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { me, page } from "../test/fixtures";
import { auditLabel } from "../lib/auditLabels";

const entries = [
  { id: 3, at: "2026-11-13T09:15:00Z", actor_email: "ana@ejemplo.es", action: "device.revoked", target_type: "device", target_id: "d1", details: { hostname: "PC-CONTA-07" }, ip: "10.0.0.9" },
  { id: 2, at: "2026-11-13T09:00:00Z", actor_email: "", action: "device.enrolled", target_type: "device", target_id: "d2", details: { hostname: "LT-VENTAS-01", token_name: "GPO" }, ip: "10.0.4.20" },
  { id: 1, at: "2026-11-13T08:00:00Z", actor_email: "luis@ejemplo.es", action: "login.failed", target_type: "", target_id: "", details: { reason: "bad_password" }, ip: "10.0.0.7" },
];

const lastAudit = (calls: { path: string }[]) => [...calls].reverse().find((c) => c.path.startsWith("/audit"))!.path;

describe("Auditoría", () => {
  it("muestra fecha, autor, acción traducida, objetivo e IP", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /audit": { body: page(entries) } });
    renderApp("/auditoria");
    const row = within((await screen.findByRole("cell", { name: "Revocó un dispositivo" })).closest("tr")!);
    expect(row.getByText("ana@ejemplo.es")).toBeInTheDocument();
    expect(row.getByRole("link", { name: "PC-CONTA-07" })).toHaveAttribute("href", "/dispositivos/d1");
    expect(row.getByText("10.0.0.9")).toBeInTheDocument();
    // Las acciones de los agentes no tienen autor humano.
    expect(within(screen.getByRole("cell", { name: "Se inscribió un dispositivo" }).closest("tr")!).getByText("Agente")).toBeInTheDocument();
  });

  it("filtra por acción y por rango de fechas", async () => {
    const calls = mockApi({ "GET /auth/me": { body: me() }, "GET /audit": { body: page(entries) } });
    renderApp("/auditoria");
    const user = userEvent.setup();
    await screen.findByRole("cell", { name: "Revocó un dispositivo" });
    await user.selectOptions(screen.getByLabelText("Acción"), "device.revoked");
    await waitFor(() => expect(lastAudit(calls)).toContain("action=device.revoked"));
    await user.type(screen.getByLabelText("Desde"), "2026-11-01");
    await user.type(screen.getByLabelText("Hasta"), "2026-11-13");
    await waitFor(() => {
      const q = new URLSearchParams(lastAudit(calls).split("?")[1]);
      expect(new Date(q.get("from")!).getDate()).toBe(1);
      // «Hasta» incluye el día entero.
      expect(new Date(q.get("to")!).getTime()).toBeGreaterThan(new Date(2026, 10, 13, 23, 59).getTime());
    });
  });

  it("filtra por autor tras dejar de escribir", async () => {
    const calls = mockApi({ "GET /auth/me": { body: me() }, "GET /audit": { body: page(entries) } });
    renderApp("/auditoria");
    await screen.findByRole("cell", { name: "Revocó un dispositivo" });
    await userEvent.setup().type(screen.getByLabelText("Autor"), "luis");
    await waitFor(() => expect(lastAudit(calls)).toContain("actor=luis"));
  });

  it("una fila se despliega para ver los detalles", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /audit": { body: page(entries) } });
    renderApp("/auditoria");
    const user = userEvent.setup();
    const row = (await screen.findByRole("cell", { name: "Intento de inicio de sesión fallido" })).closest("tr")!;
    await user.click(within(row).getByText("Detalles"));
    expect(within(row).getByText(/bad_password/)).toBeVisible();
  });

  it("sin entradas lo dice", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /audit": { body: page([]) } });
    renderApp("/auditoria");
    expect(await screen.findByText("No hay entradas que coincidan.")).toBeInTheDocument();
  });
});

describe("auditLabel", () => {
  it("traduce cada acción conocida y deja tal cual las desconocidas", () => {
    for (const a of ["login.ok", "login.failed", "logout", "totp.enrolled", "admin.created", "admin.updated", "token.created",
      "token.revoked", "device.enrolled", "device.reenrolled", "device.updated", "device.revoked", "device.deleted",
      "group.created", "group.updated", "group.deleted", "devices.exported"]) {
      expect(auditLabel(a)).not.toBe(a);
    }
    expect(auditLabel("device.revoked")).toBe("Revocó un dispositivo");
    expect(auditLabel("algo.nuevo")).toBe("algo.nuevo");
  });
});
