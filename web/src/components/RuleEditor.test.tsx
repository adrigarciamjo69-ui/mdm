import { describe, expect, it } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import RuleEditor, { type Rule } from "./RuleEditor";
import { mockApi } from "../test/mockApi";

function Harness() {
  const [rule, setRule] = useState<Rule>({ match: "all", conditions: [{ field: "platform", op: "eq", value: "windows" }] });
  return <RuleEditor rule={rule} onChange={setRule} groups={[{ id: "g1", name: "Ventas" }]} />;
}

describe("RuleEditor", () => {
  it("ofrece operadores de versión para os_version y de texto para hostname", async () => {
    mockApi({ "POST /groups/preview": { body: { count: 0, sample: [] } } });
    render(<Harness />);
    const user = userEvent.setup();
    const row = screen.getAllByRole("group", { name: /Condición/ })[0];
    await user.selectOptions(within(row).getByLabelText("Campo"), "os_version");
    expect(within(row).getAllByRole("option").map((o) => o.textContent)).toEqual(expect.arrayContaining(["es mayor o igual que", "es menor que"]));
    expect(within(row).queryByRole("option", { name: "contiene" })).toBeNull();
    await user.selectOptions(within(row).getByLabelText("Campo"), "hostname");
    expect(within(row).getByRole("option", { name: "contiene" })).toBeInTheDocument();
  });

  it("pide la vista previa tras dejar de editar y muestra el recuento", async () => {
    const calls = mockApi({ "POST /groups/preview": { body: { count: 14, sample: [{ id: "d1", hostname: "PC-CONTA-01" }] } } });
    render(<Harness />);
    expect(await screen.findByText(/14 equipos cumplen la regla/)).toBeInTheDocument();
    expect(screen.getByText(/PC-CONTA-01/)).toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Añadir condición" }));
    const second = screen.getAllByRole("group", { name: /Condición/ })[1];
    await user.type(within(second).getByLabelText("Valor"), "conta");
    await waitFor(() => expect(calls.filter((c) => c.path === "/groups/preview").length).toBeGreaterThan(1));
    const last = calls.filter((c) => c.path === "/groups/preview").pop()!.body as { rule: Rule };
    expect(last.rule.conditions).toHaveLength(2);
  });
});
