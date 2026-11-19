import { describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { me } from "../test/fixtures";

const future = new Date(Date.now() + 5 * 86_400_000).toISOString();
const tokens = [
  { id: "t1", name: "Despliegue GPO", state: "active", group_id: null, group_name: "", expires_at: future, max_uses: 50, uses: 3, created_at: "2026-11-10T08:00:00Z" },
  { id: "t2", name: "Portátiles", state: "exhausted", group_id: "g2", group_name: "Ventas", expires_at: null, max_uses: 5, uses: 5, created_at: "2026-11-01T08:00:00Z" },
  { id: "t3", name: "Prueba", state: "expired", group_id: null, group_name: "", expires_at: "2026-11-01T00:00:00Z", max_uses: null, uses: 7, created_at: "2026-10-01T08:00:00Z" },
  { id: "t4", name: "Viejo", state: "revoked", group_id: null, group_name: "", expires_at: null, max_uses: null, uses: 0, created_at: "2026-09-01T08:00:00Z" },
];
const created = {
  ...tokens[0], id: "t9", name: "Oficina Girona", uses: 0, token: "Zt0k3n-en-claro-que-solo-se-ve-una-vez-AAAA",
  install: {
    linux: "curl -fsSL https://mdm.ejemplo.es/downloads/install.sh | sudo sh -s -- --server https://mdm.ejemplo.es --token Zt0k3n-en-claro-que-solo-se-ve-una-vez-AAAA",
    windows: 'powershell -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create((irm https://mdm.ejemplo.es/downloads/install.ps1))) -Server https://mdm.ejemplo.es -Token Zt0k3n-en-claro-que-solo-se-ve-una-vez-AAAA"',
  },
};

function setup() {
  let list = [...tokens];
  return mockApi({
    "GET /auth/me": { body: me("operator") },
    "GET /groups": { body: [{ id: "g2", name: "Ventas", description: "", device_count: 0 }] },
    "GET /enrollment-tokens": () => ({ body: list }),
    "POST /enrollment-tokens": () => { const { token: _t, install: _i, ...row } = created; list = [row, ...list]; return { status: 201, body: created }; },
    "POST /enrollment-tokens/t1/revoke": () => { list = list.map((t) => (t.id === "t1" ? { ...t, state: "revoked" } : t)); return { status: 204 }; },
  });
}

describe("Inscripción", () => {
  it("la tabla muestra estado, usos y caducidad", async () => {
    setup();
    renderApp("/inscripcion");
    const r1 = within((await screen.findByText("Despliegue GPO")).closest("tr")!);
    expect(r1.getByText("Activo")).toBeInTheDocument();
    expect(r1.getByText("3 de 50")).toBeInTheDocument();
    expect(r1.getByText(/dentro de 5 días|en 5 días/)).toBeInTheDocument();
    expect(within(screen.getByText("Portátiles").closest("tr")!).getByText("Agotado")).toBeInTheDocument();
    expect(within(screen.getByText("Portátiles").closest("tr")!).getByText("Ventas")).toBeInTheDocument();
    const r3 = within(screen.getByText("Prueba").closest("tr")!);
    expect(r3.getByText("Caducado")).toBeInTheDocument();
    expect(r3.getByText("7")).toBeInTheDocument();
    expect(within(screen.getByText("Viejo").closest("tr")!).getByText("Revocado")).toBeInTheDocument();
    // Solo los activos se pueden revocar.
    expect(screen.getAllByRole("button", { name: "Revocar" })).toHaveLength(1);
  });

  it("el formulario ofrece caducidad en 1, 7 o 30 días o sin caducidad, y 7 días por defecto", async () => {
    setup();
    renderApp("/inscripcion");
    const sel = await screen.findByLabelText("Caducidad");
    expect(sel).toHaveValue("7");
    expect(within(sel).getAllByRole("option").map((o) => o.textContent)).toEqual(["1 día", "7 días", "30 días", "Sin caducidad"]);
  });

  it("crear un token envía nombre, grupo, caducidad y usos", async () => {
    const calls = setup();
    renderApp("/inscripcion");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Oficina Girona");
    await screen.findByRole("option", { name: "Ventas" });
    await user.selectOptions(screen.getByLabelText("Grupo de destino"), "g2");
    await user.selectOptions(screen.getByLabelText("Caducidad"), "30");
    await user.type(screen.getByLabelText(/Máximo de equipos/), "40");
    const before = Date.now();
    await user.click(screen.getByRole("button", { name: "Crear token" }));
    await screen.findByText("Zt0k3n-en-claro-que-solo-se-ve-una-vez-AAAA");
    const body = calls.find((c) => c.method === "POST")!.body as Record<string, unknown>;
    expect(body).toMatchObject({ name: "Oficina Girona", group_id: "g2", max_uses: 40 });
    const exp = new Date(body.expires_at as string).getTime();
    expect(exp - before).toBeGreaterThan(29.9 * 86_400_000);
    expect(exp - before).toBeLessThan(30.1 * 86_400_000);
  });

  it("sin caducidad ni máximo no envía esos campos", async () => {
    const calls = setup();
    renderApp("/inscripcion");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Sin límites");
    await user.selectOptions(screen.getByLabelText("Caducidad"), "");
    await user.click(screen.getByRole("button", { name: "Crear token" }));
    await screen.findByText(/no se volverá a mostrar/);
    expect(calls.find((c) => c.method === "POST")!.body).toEqual({ name: "Sin límites" });
  });

  it("crear un token muestra el token y los dos comandos una sola vez, con aviso de que no volverán a verse", async () => {
    setup();
    renderApp("/inscripcion");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Oficina Girona");
    await user.click(screen.getByRole("button", { name: "Crear token" }));
    const reveal = await screen.findByRole("region", { name: "Token creado: Oficina Girona" });
    expect(reveal).toHaveTextContent("no se volverá a mostrar");
    expect(within(reveal).getByText(created.install.linux)).toBeInTheDocument();
    expect(within(reveal).getByText(created.install.windows)).toBeInTheDocument();
    expect(within(reveal).getAllByRole("button", { name: "Copiar" })).toHaveLength(3);
  });

  it("al cerrar el aviso el token ya no aparece en la página", async () => {
    setup();
    renderApp("/inscripcion");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre"), "Oficina Girona");
    await user.click(screen.getByRole("button", { name: "Crear token" }));
    await screen.findByRole("region", { name: /Token creado/ });
    await user.click(screen.getByRole("button", { name: "Ya lo he copiado" }));
    expect(screen.queryByText(/Zt0k3n/)).toBeNull();
    expect(screen.getByText("Oficina Girona")).toBeInTheDocument();
  });

  it("revocar pide confirmación y actualiza el estado", async () => {
    setup();
    renderApp("/inscripcion");
    const user = userEvent.setup();
    const row = (await screen.findByText("Despliegue GPO")).closest("tr")!;
    await user.click(within(row).getByRole("button", { name: "Revocar" }));
    const dialog = screen.getByRole("dialog", { name: "Revocar el token «Despliegue GPO»" });
    expect(dialog).toHaveTextContent("Los equipos ya inscritos no se ven afectados");
    await user.click(within(dialog).getByRole("button", { name: "Revocar" }));
    await waitFor(() => expect(within(screen.getByText("Despliegue GPO").closest("tr")!).getByText("Revocado")).toBeInTheDocument());
  });
});

describe("CopyField", () => {
  it("copia al portapapeles y muestra «Copiado» un momento", async () => {
    const { default: CopyField } = await import("../components/CopyField");
    const { render } = await import("@testing-library/react");
    const user = userEvent.setup();
    const write = vi.spyOn(navigator.clipboard, "writeText");
    render(<CopyField label="Token" value="abc" />);
    await user.click(screen.getByRole("button", { name: "Copiar" }));
    expect(write).toHaveBeenCalledWith("abc");
    expect(screen.getByRole("button", { name: "Copiado" })).toBeInTheDocument();
  });
});
