import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import MergeDialog from "./MergeDialog";
import { mockApi } from "../test/mockApi";
import { AppProviders } from "../App";
import { me } from "../test/fixtures";
import type { Asset, Device } from "../api/types";

const base = (o: Partial<Asset>): Asset => ({
  id: "x", device_id: null, origin: "manual", category_id: "c1", category: "Portátil", category_auto: false, name: "", brand: "", model: "",
  serial: "", asset_tag: "", status: "en_uso", owner: null, department: "", location: "", price: null, purchase_date: "", warranty_until: "",
  purchase_order: "", supplier: "", notes: "", deleted_at: null, deleted_reason: "", owner_locked: false, department_locked: false, created_at: "2026-11-25T09:00:00Z",
  updated_at: "2026-11-25T09:00:00Z", device: null, ...o,
});
const manual = base({ id: "m1", name: "Portátil de Ana", asset_tag: "EL-7", location: "Bilbao", price: 899, notes: "Factura en Documentos" });
const agent = base({ id: "g1", origin: "agent", device_id: "d1", device: { id: "d1" } as Device, name: "PC-ANA-01", brand: "Dell", model: "Latitude 5440",
  serial: "ABC123", notes: "Inscrito por token" });

function renderDialog(a: Asset, b: Asset, onDone = vi.fn()) {
  const calls = mockApi({ "GET /auth/me": { body: me() }, "POST /assets/merge": { body: { ...agent, name: "Portátil de Ana" } } });
  render(<MemoryRouter><AppProviders><MergeDialog a={a} b={b} onClose={vi.fn()} onDone={onDone} /></AppProviders></MemoryRouter>);
  return calls;
}

describe("MergeDialog", () => {
  it("conserva por defecto la ficha con agente, rellena los vacíos con la otra y deja elegir cada campo", async () => {
    const done = vi.fn();
    const calls = renderDialog(manual, agent, done);
    const user = userEvent.setup();
    expect(screen.getByRole("radio", { name: /Conservar.*PC-ANA-01/ })).toBeChecked();
    // Etiqueta, ubicación y precio solo los tiene el manual: vienen de él.
    expect(screen.getByRole("radio", { name: "Etiqueta interna: EL-7" })).toBeChecked();
    // Nombre: por defecto el del conservado; se elige el del manual.
    await user.click(screen.getByRole("radio", { name: "Nombre: Portátil de Ana" }));
    await user.click(screen.getByRole("checkbox", { name: /Juntar las notas/ }));
    await user.click(screen.getByRole("button", { name: "Fusionar" }));
    await waitFor(() => expect(done).toHaveBeenCalledWith("g1"));
    const body = calls.find((c) => c.path === "/assets/merge")!.body as { keep_id: string; other_id: string; from_other: string[]; notes_both: boolean };
    expect(body.keep_id).toBe("g1");
    expect(body.other_id).toBe("m1");
    expect([...body.from_other].sort()).toEqual(["asset_tag", "location", "name", "price"]);
    expect(body.notes_both).toBe(true);
  });

  it("un número de serie de relleno del agente no gana al real", () => {
    renderDialog({ ...manual, serial: "MP100251222K00270" }, { ...agent, serial: "Default string" });
    expect(screen.getByRole("radio", { name: "Número de serie: MP100251222K00270" })).toBeChecked();
  });

  it("dos equipos con agente no se pueden fusionar", () => {
    renderDialog(agent, { ...agent, id: "g2", device_id: "d2", name: "PC-ANA-02" });
    expect(screen.getByText(/dos equipos con agente/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Fusionar" })).toBeDisabled();
  });
});
