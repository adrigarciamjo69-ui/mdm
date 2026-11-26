import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import AssetForm from "./AssetForm";
import { mockApi } from "../test/mockApi";
import { AppProviders } from "../App";
import { me } from "../test/fixtures";
import type { Asset } from "../api/types";

const asset = (owner: Asset["owner"]): Asset => ({
  id: "a1", device_id: null, origin: "manual", category_id: "c1", category: "Portátil", category_auto: false, name: "Portátil 1",
  brand: "", model: "", serial: "", asset_tag: "", status: "en_uso", owner, department: "Escrito a mano", location: "Sede Bilbao",
  price: null, purchase_date: "", warranty_until: "", purchase_order: "", supplier: "", notes: "", deleted_at: null, deleted_reason: "",
  created_at: "2026-11-25T09:00:00Z", updated_at: "2026-11-25T09:00:00Z", device: null, owner_locked: false, department_locked: false,
});

function renderForm(a: Asset, onSave = vi.fn()) {
  mockApi({ "GET /auth/me": { body: me() }, "GET /asset-categories": { body: [{ id: "c1", name: "Portátil", assets: 1 }] } });
  render(<MemoryRouter><AppProviders><AssetForm asset={a} onSave={onSave} onCancel={vi.fn()} /></AppProviders></MemoryRouter>);
}

describe("AssetForm · departamento", () => {
  it("con una persona de Entra con departamento, se muestra el suyo y no se edita", () => {
    renderForm(asset({ id: "u1", name: "Ana", email: "ana@electrans.es", department: "Finanzas" }));
    const dep = screen.getByLabelText(/Departamento/);
    expect(dep).toHaveValue("Finanzas");
    expect(dep).toBeDisabled();
    expect(screen.getByLabelText("Ubicación")).toBeEnabled();
  });
  it("sin persona (o sin departamento en Entra) se escribe a mano", () => {
    renderForm(asset(null));
    expect(screen.getByLabelText("Departamento")).toHaveValue("Escrito a mano");
    expect(screen.getByLabelText("Departamento")).toBeEnabled();
  });
});

describe("AssetForm · bloqueos", () => {
  const ana = { id: "u1", name: "Ana", email: "ana@electrans.es", department: "Finanzas" };
  it("bloquear el departamento permite escribirlo partiendo del de Entra y se envía bloqueado", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderForm(asset(ana), onSave);
    const locks = screen.getAllByLabelText("Bloquear");
    fireEvent.click(locks[locks.length - 1]);
    const dep = screen.getByLabelText(/Departamento/);
    expect(dep).toBeEnabled();
    expect(dep).toHaveValue("Finanzas");
    fireEvent.change(dep, { target: { value: "Taller" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(onSave).toHaveBeenCalled());
    expect(onSave.mock.calls[0][0]).toMatchObject({ department: "Taller", department_locked: true, owner_id: "u1" });
  });
  it("en un equipo con agente la persona se puede bloquear y, si no, sigue la sesión", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderForm({ ...asset(ana), origin: "agent", device_id: "d1" }, onSave);
    expect(screen.getByText("Sigue al usuario con sesión en el equipo")).toBeInTheDocument();
    fireEvent.click(screen.getAllByLabelText("Bloquear")[0]);
    expect(screen.queryByText("Sigue al usuario con sesión en el equipo")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(onSave).toHaveBeenCalled());
    expect(onSave.mock.calls[0][0]).toMatchObject({ owner_locked: true, department_locked: false, department: "Finanzas" });
  });
});
