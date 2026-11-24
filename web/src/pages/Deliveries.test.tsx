import { describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { asset, dirUser, me, page } from "../test/fixtures";
import type { Delivery } from "../api/types";
import * as pdf from "../lib/deliveryPdf";

const settings = { company: "Electrans S.A.", nif: "", address1: "", address2: "", phone: "", web: "", email: "", city: "Barcelona", logo: "",
  colors: { entrega: { primary: "#1c3ca8", footer: "#0f172a" }, devolucion: { primary: "#b45309", footer: "#0f172a" } },
  clauses: { entrega: ["Cláusula."], devolucion: ["Cláusula."] } };

function delivery(over: Partial<Delivery> = {}): Delivery {
  return {
    id: "d1", number: "", type: "entrega", status: "borrador",
    user: { id: "u1", name: "Ana Pérez", email: "ana@electrans.es", department: "TIC", job_title: "Técnica", employee_id: "E-1" },
    recipient_dni: "12345678Z", date: "2026-11-24", responsible: "it@electrans.es", notes: "", cancel_reason: "", created_by: "it@electrans.es",
    created_at: "2026-11-24T08:00:00Z", confirmed_at: null,
    items: [{ id: "i1", asset_id: "a1", category: "Monitor", brand: "Dell", model: "P2422H", serial: "CN-0ABC", observations: "" }], ...over,
  };
}
const base = (role: "admin" | "operator" | "readonly" = "operator") => ({
  "GET /auth/me": { body: me(role) }, "GET /alerts/counts": { body: {} }, "GET /delivery-settings": { body: settings },
});

describe("Doc. Entrega", () => {
  it("lista las actas con número, tipo, persona y estado", async () => {
    mockApi({ ...base(), "GET /deliveries": { body: page([delivery({ id: "d2", number: "ENT-2026-0001", status: "confirmada" }), delivery()]) } });
    renderApp("/actas");
    const row = within((await screen.findByRole("link", { name: "ENT-2026-0001" })).closest("tr")!);
    expect(row.getByText("Entrega")).toBeInTheDocument();
    expect(row.getByText("Ana Pérez")).toBeInTheDocument();
    expect(row.getByText("Confirmada")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Borrador" })).toHaveAttribute("href", "/actas/d1");
  });

  it("una devolución desde la ficha de la persona trae sus activos", async () => {
    const calls = mockApi({ ...base(),
      "GET /directory/users/u1": { body: { ...dirUser(), assets: [asset({ owner: { id: "u1", name: "Ana Pérez", email: "" } })], licenses: [], deliveries: [] } },
      "POST /deliveries": (c: { body: unknown }) => ({ status: 201, body: delivery({ id: "d9", type: "devolucion", ...(c.body as object) }) }),
      "GET /deliveries/d9": { body: delivery({ id: "d9", type: "devolucion" }) } });
    const { location } = renderApp("/actas/nueva?tipo=devolucion&persona=u1");
    expect(await screen.findByText("Ana Pérez")).toBeInTheDocument();
    expect(await screen.findByDisplayValue("P2422H")).toBeInTheDocument();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("DNI / NIE del receptor"), "12345678z");
    await user.click(screen.getByRole("button", { name: "Guardar borrador" }));
    await waitFor(() => expect(calls.find((c) => c.method === "POST" && c.path === "/deliveries")?.body).toMatchObject({
      type: "devolucion", user_id: "u1", recipient_dni: "12345678z",
      items: [{ asset_id: "a1", category: "Monitor", brand: "Dell", model: "P2422H", serial: "CN-0ABC", observations: "" }],
    }));
    await waitFor(() => expect(location()).toBe("/actas/d9"));
  });

  it("añade una línea libre y descarga la vista previa", async () => {
    const save = vi.spyOn(pdf, "downloadDeliveryPdf").mockImplementation(() => {});
    mockApi({ ...base(), "GET /directory/users/u1": { body: { ...dirUser(), assets: [], licenses: [], deliveries: [] } } });
    renderApp("/actas/nueva?tipo=entrega&persona=u1");
    const user = userEvent.setup();
    await screen.findByText("Ana Pérez");
    await user.click(screen.getByRole("button", { name: "Añadir línea sin activo" }));
    await user.type(screen.getByLabelText("Modelo de la línea 1"), "Cargador USB-C");
    await user.click(screen.getByRole("button", { name: "Vista previa PDF" }));
    await waitFor(() => expect(save).toHaveBeenCalled());
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ number: "", type: "entrega", user: { name: "Ana Pérez" },
      items: [expect.objectContaining({ model: "Cargador USB-C" })] }), expect.objectContaining({ company: "Electrans S.A." }));
    save.mockRestore();
  });

  it("confirma un borrador y después solo deja descargar el PDF", async () => {
    let confirmed = false;
    const calls = mockApi({ ...base(),
      "GET /deliveries/d1": () => ({ body: confirmed ? delivery({ status: "confirmada", number: "ENT-2026-0001" }) : delivery() }),
      "PATCH /deliveries/d1": { body: delivery() },
      "POST /deliveries/d1/confirm": () => { confirmed = true; return { body: delivery({ status: "confirmada", number: "ENT-2026-0001" }) }; } });
    renderApp("/actas/d1");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Confirmar acta" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Confirmar" }));
    await waitFor(() => expect(calls.some((c) => c.path === "/deliveries/d1/confirm")).toBe(true));
    // Antes de confirmar se guardan los cambios del borrador.
    expect(calls.findIndex((c) => c.method === "PATCH")).toBeLessThan(calls.findIndex((c) => c.path === "/deliveries/d1/confirm"));
    expect(await screen.findByRole("heading", { name: /ENT-2026-0001/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Descargar PDF" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Guardar borrador" })).toBeNull();
  });

  it("solo lectura ve el acta pero no puede crearla", async () => {
    mockApi({ ...base("readonly"), "GET /deliveries": { body: page([delivery()]) } });
    renderApp("/actas");
    await screen.findByRole("link", { name: "Borrador" });
    expect(screen.queryByRole("link", { name: "Nueva acta" })).toBeNull();
  });
});
