import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import RunDialog from "./RunDialog";
import { mockApi } from "../test/mockApi";
import { AppProviders } from "../App";
import { MemoryRouter } from "react-router-dom";
import { me } from "../test/fixtures";

const scripts = [{ id: "s1", name: "Limpiar", platform: "linux", interpreter: "bash", timeout_s: 300, latest_version: 2, archived: false,
  parameters: [{ name: "RUTA", description: "Carpeta", required: true, default: "" }] }];

function renderDialog(type: string, onDone = vi.fn()) {
  const calls = mockApi({
    "GET /auth/me": { body: me() },
    "GET /scripts": { body: scripts },
    "POST /batches": { status: 201, body: { batch: { id: "b1" }, skipped: [] } },
  });
  render(
    <MemoryRouter>
      <AppProviders>
        <RunDialog type={type as never} target={{ deviceIds: ["d1", "d2", "d3"], label: "3 equipos seleccionados" }} onClose={vi.fn()} onDone={onDone} />
      </AppProviders>
    </MemoryRouter>,
  );
  return calls;
}

describe("RunDialog", () => {
  it("reiniciar envía los equipos, el retraso y el mensaje", async () => {
    const done = vi.fn();
    const calls = renderDialog("reboot", done);
    const user = userEvent.setup();
    expect(await screen.findByText(/Se reiniciará.*3 equipos seleccionados/)).toBeInTheDocument();
    await user.selectOptions(screen.getByLabelText("Retraso"), "300");
    await user.type(screen.getByLabelText("Mensaje para el usuario"), "Mantenimiento");
    await user.click(screen.getByRole("button", { name: "Reiniciar" }));
    await waitFor(() => expect(done).toHaveBeenCalledWith("b1"));
    expect(calls.find((c) => c.path === "/batches")!.body).toEqual({ type: "reboot", device_ids: ["d1", "d2", "d3"], params: { delay_s: 300, message: "Mantenimiento" } });
  });

  it("ejecutar un script con un parámetro obligatorio vacío no se envía", async () => {
    const calls = renderDialog("run_script");
    const user = userEvent.setup();
    await screen.findByRole("option", { name: "Limpiar" });
    await user.selectOptions(screen.getByLabelText("Script"), "s1");
    await user.click(screen.getByRole("button", { name: "Ejecutar" }));
    expect(await screen.findByText("Falta el parámetro obligatorio RUTA.")).toBeInTheDocument();
    expect(calls.some((c) => c.path === "/batches")).toBe(false);
    await user.type(screen.getByLabelText("RUTA"), "/tmp");
    await user.click(screen.getByRole("button", { name: "Ejecutar" }));
    await waitFor(() => expect(calls.find((c) => c.path === "/batches")?.body).toEqual({
      type: "run_script", device_ids: ["d1", "d2", "d3"], params: { script_id: "s1", version: 0, params: { RUTA: "/tmp" } },
    }));
  });

  it("retirar exige escribir el número de equipos", async () => {
    renderDialog("retire");
    const user = userEvent.setup();
    const btn = await screen.findByRole("button", { name: "Retirar" });
    expect(btn).toBeDisabled();
    await user.type(screen.getByRole("textbox"), "3");
    expect(btn).toBeEnabled();
  });
});

it("solo ofrece scripts de la plataforma del destino", async () => {
  mockApi({
    "GET /auth/me": { body: me() },
    "GET /scripts": { body: [...scripts, { ...scripts[0], id: "s2", name: "Revisar Defender", platform: "windows" }] },
  });
  render(
    <MemoryRouter>
      <AppProviders>
        <RunDialog type="run_script" target={{ deviceIds: ["d1"], label: "PC-1", platform: "windows" }} onClose={vi.fn()} onDone={vi.fn()} />
      </AppProviders>
    </MemoryRouter>,
  );
  expect(await screen.findByRole("option", { name: "Revisar Defender" })).toBeInTheDocument();
  expect(screen.queryByRole("option", { name: "Limpiar" })).toBeNull();
});

it("con un solo equipo, retirar exige escribir su nombre", async () => {
  mockApi({ "GET /auth/me": { body: me() }, "GET /scripts": { body: [] } });
  render(
    <MemoryRouter>
      <AppProviders>
        <RunDialog type="retire" target={{ deviceIds: ["d1"], label: "PC-CONTA-04" }} onClose={vi.fn()} onDone={vi.fn()} />
      </AppProviders>
    </MemoryRouter>,
  );
  const user = userEvent.setup();
  const btn = await screen.findByRole("button", { name: "Retirar" });
  await user.type(screen.getByRole("textbox"), "1");
  expect(btn).toBeDisabled();
  await user.clear(screen.getByRole("textbox"));
  await user.type(screen.getByRole("textbox"), "PC-CONTA-04");
  expect(btn).toBeEnabled();
});
