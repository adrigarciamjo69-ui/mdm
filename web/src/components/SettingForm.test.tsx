import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import SettingForm from "./SettingForm";
import type { CatalogDef } from "../api/types";

const def: CatalogDef = {
  key: "windows.power", platform: "windows", title: "Energía", description: "d", audit_only: false, enforce_only: false,
  fields: [
    { name: "plan", label: "Plan de energía", type: "enum", required: false, options: ["balanced", "high_performance"],
      option_labels: { balanced: "Equilibrado", high_performance: "Alto rendimiento" } },
    { name: "domain", label: "Dominio", type: "string", required: true, description: "Nombre DNS completo." },
  ],
};

describe("SettingForm", () => {
  it("muestra las opciones con su etiqueta y la ayuda del campo", () => {
    render(<SettingForm def={def} setting={{ key: "windows.power", mode: "enforce", params: {} }} errors={{}} onChange={vi.fn()} onRemove={vi.fn()} />);
    expect(screen.getByRole("option", { name: "Alto rendimiento" })).toHaveValue("high_performance");
    expect(screen.getByText("Nombre DNS completo.")).toBeInTheDocument();
  });
});
