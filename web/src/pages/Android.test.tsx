import { describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { detail, me } from "../test/fixtures";

describe("Integración con Android", () => {
  it("sin clave pide subirla; al guardarla envía el JSON", async () => {
    const calls = mockApi({
      "GET /auth/me": { body: me() }, "GET /integrations/android": { body: { configured: false, linked: false } },
      "PUT /integrations/android/key": { body: { configured: true, linked: false, project_id: "mdm-empresa", client_email: "mdm@x" } },
    });
    renderApp("/integraciones/android");
    const user = userEvent.setup();
    const area = await screen.findByLabelText("Clave JSON de la cuenta de servicio");
    await user.click(area);
    await user.paste('{"type":"service_account"}');
    await user.click(screen.getByRole("button", { name: "Guardar clave" }));
    await waitFor(() => expect(calls.find((c) => c.method === "PUT")?.body).toEqual({ key_json: '{"type":"service_account"}' }));
    expect(await screen.findByText(/mdm-empresa/)).toBeInTheDocument();
  });

  it("vincular lleva a la dirección de Google", async () => {
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign, search: "" });
    mockApi({
      "GET /auth/me": { body: me() }, "GET /integrations/android": { body: { configured: true, linked: false, project_id: "p", client_email: "e" } },
      "POST /integrations/android/signup": { body: { url: "https://play.google.com/work/adminsignup?token=X" } },
    });
    renderApp("/integraciones/android");
    await userEvent.setup().click(await screen.findByRole("button", { name: "Vincular con Google" }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith("https://play.google.com/work/adminsignup?token=X"));
    vi.unstubAllGlobals();
  });

  it("vinculada muestra la empresa, la última sincronización y su error", async () => {
    mockApi({
      "GET /auth/me": { body: me() },
      "GET /integrations/android": { body: { configured: true, linked: true, project_id: "p", client_email: "e", enterprise_name: "enterprises/LC01",
        enterprise_display_name: "Ejemplo SL", last_sync_at: "2026-11-18T10:00:00Z", last_sync_error: "Google respondió 429: cuota" } },
    });
    renderApp("/integraciones/android");
    expect(await screen.findByText("Ejemplo SL")).toBeInTheDocument();
    expect(screen.getByText(/Google respondió 429/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Desvincular" })).toBeInTheDocument();
  });
});

describe("Inscripción de móviles", () => {
  it("crear un token muestra su código QR", async () => {
    const calls = mockApi({
      "GET /auth/me": { body: me("operator") }, "GET /groups": { body: [{ id: "g1", name: "Repartidores", kind: "static", device_count: 0 }] },
      "GET /tokens": { body: [] }, "GET /android/enrollment-tokens": { body: [] },
      "GET /integrations/android": { status: 403, body: { error: { code: "forbidden", message: "x" } } },
      "POST /android/enrollment-tokens": { status: 201, body: { id: "t1", name: "Furgonetas", qr_code: '{"a":1}', group_id: "g1", group_name: "Repartidores",
        one_time: false, expires_at: "2026-11-25T10:00:00Z", revoked_at: null, created_at: "2026-11-18T10:00:00Z" } },
    });
    renderApp("/inscripcion?plataforma=android");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Nombre del token"), "Furgonetas");
    await user.selectOptions(screen.getByLabelText("Grupo de destino"), "g1");
    await user.click(screen.getByRole("button", { name: "Crear token" }));
    await waitFor(() => expect(calls.find((c) => c.path === "/android/enrollment-tokens" && c.method === "POST")?.body).toEqual({
      name: "Furgonetas", group_id: "g1", duration_days: 7, one_time: false }));
    expect(await screen.findByRole("img", { name: /Código QR de inscripción/ })).toBeInTheDocument();
  });
});

describe("Ficha de un móvil", () => {
  const phone = { ...detail(), platform: "android", os_name: "Android", os_version: "14", hostname: "Samsung SM-A556B (1234)",
    inventory: { ...detail().inventory, mobile: { state: "ACTIVE", imei: "350000000000001", phone_number: "+34600000000", carrier: "Movistar",
      security_patch_level: "2026-10-01", integrity: "SECURE", device_secure: true, developer_options: false, unknown_sources: false, storage_bytes: 0,
      policy_applied: true, build_number: "", meid: "", last_status_report: "" } } };

  it("muestra los datos del móvil y sus acciones (no las del agente)", async () => {
    mockApi({ "GET /auth/me": { body: me("admin") }, "GET /devices/d1": { body: phone }, "GET /groups": { body: [] } });
    renderApp("/dispositivos/d1");
    expect(await screen.findByText("350000000000001")).toBeInTheDocument();
    expect(screen.getByText("2026-10-01")).toBeInTheDocument();
    const bar = screen.getByRole("toolbar", { name: "Acciones" });
    for (const name of ["Bloquear", "Reiniciar", "Cambiar contraseña", "Modo perdido", "Borrar el móvil"]) {
      expect(within(bar).getByRole("button", { name })).toBeInTheDocument();
    }
    expect(within(bar).queryByRole("button", { name: "Ejecutar script" })).toBeNull();
    expect(within(bar).queryByRole("button", { name: "Apagar" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Revocar" })).toBeNull();
    expect(screen.queryByRole("tab", { name: "Actualizaciones" })).toBeNull();
    expect(screen.queryByText("Dominio")).toBeNull();
  });

  it("borrar exige escribir el nombre y envía la orden wipe", async () => {
    const calls = mockApi({ "GET /auth/me": { body: me("admin") }, "GET /devices/d1": { body: phone }, "GET /groups": { body: [] },
      "POST /batches": { status: 201, body: { batch: { id: "b1" }, skipped: [] } } });
    renderApp("/dispositivos/d1");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Borrar el móvil" }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByRole("button", { name: "Borrar el móvil" })).toBeDisabled();
    await user.type(within(dialog).getByLabelText(/Escribe/), "Samsung SM-A556B (1234)");
    await user.click(within(dialog).getByRole("button", { name: "Borrar el móvil" }));
    await waitFor(() => expect(calls.find((c) => c.path === "/batches")?.body).toEqual({ type: "wipe", device_ids: ["d1"], params: { wipe_external_storage: false } }));
  });
});
