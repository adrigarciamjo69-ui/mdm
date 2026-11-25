import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
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
  created_at: "2026-11-25T09:00:00Z", updated_at: "2026-11-25T09:00:00Z", device: null,
});

function renderForm(a: Asset) {
  mockApi({ "GET /auth/me": { body: me() }, "GET /asset-categories": { body: [{ id: "c1", name: "Portátil", assets: 1 }] } });
  render(<MemoryRouter><AppProviders><AssetForm asset={a} onSave={vi.fn()} onCancel={vi.fn()} /></AppProviders></MemoryRouter>);
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
