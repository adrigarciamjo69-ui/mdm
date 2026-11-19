import { describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { device, me, page } from "../test/fixtures";

const lastList = (calls: { path: string }[]) => [...calls].reverse().find((c) => c.path.startsWith("/devices?") || c.path === "/devices")!.path;

function setup(items = [device()], total?: number) {
  return mockApi({
    "GET /auth/me": { body: me() },
    "GET /groups": { body: [{ id: "g1", name: "Contabilidad", description: "", device_count: 1 }] },
    "GET /devices": (c) => {
      const n = Number(new URLSearchParams(c.path.split("?")[1] ?? "").get("page") ?? 1);
      return { body: page(items, { total: total ?? items.length, page: n }) };
    },
  });
}

describe("Dispositivos", () => {
  it("muestra nombre, sistema, estado, cumplimiento, grupo, usuario y última conexión", async () => {
    setup();
    renderApp("/dispositivos");
    const row = (await screen.findByRole("link", { name: "PC-CONTA-07" })).closest("tr")!;
    const r = within(row);
    expect(r.getByText(/Windows 11 Pro/)).toBeInTheDocument();
    expect(r.getByText("En línea")).toBeInTheDocument();
    expect(r.getByText("Contabilidad")).toBeInTheDocument();
    expect(r.getByText("ana.garcia")).toBeInTheDocument();
    expect(r.getByText(/hace 4\d s/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "PC-CONTA-07" })).toHaveAttribute("href", "/dispositivos/d1");
  });

  it("escribir en la búsqueda pide ?q=… tras 300 ms y vuelve a la página 1", async () => {
    const calls = setup([device()], 60);
    const { location } = renderApp("/dispositivos?page=2");
    await screen.findByRole("link", { name: "PC-CONTA-07" });
    await userEvent.setup().type(screen.getByRole("searchbox", { name: "Buscar" }), "conta");
    await waitFor(() => expect(lastList(calls)).toContain("q=conta"));
    expect(lastList(calls)).not.toContain("page=2");
    expect(location()).toContain("q=conta");
    // No se lanza una petición por cada tecla.
    expect(calls.filter((c) => c.path.includes("q=c")).length).toBeLessThanOrEqual(2);
  });

  it("elegir «Windows» añade platform=windows a la URL y a la petición", async () => {
    const calls = setup();
    const { location } = renderApp("/dispositivos");
    await screen.findByRole("link", { name: "PC-CONTA-07" });
    await userEvent.setup().selectOptions(screen.getByLabelText("Plataforma"), "windows");
    await waitFor(() => expect(lastList(calls)).toContain("platform=windows"));
    expect(location()).toContain("platform=windows");
  });

  it("pulsar la cabecera «Nombre» alterna asc y desc", async () => {
    const calls = setup();
    renderApp("/dispositivos");
    await screen.findByRole("link", { name: "PC-CONTA-07" });
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /Nombre/ }));
    await waitFor(() => expect(lastList(calls)).toContain("order=desc"));
    expect(screen.getByRole("columnheader", { name: /Nombre/ })).toHaveAttribute("aria-sort", "descending");
    await user.click(screen.getByRole("button", { name: /Nombre/ }));
    // Volver al orden por defecto reutiliza la primera respuesta, ya en caché.
    await waitFor(() => expect(screen.getByRole("columnheader", { name: /Nombre/ })).toHaveAttribute("aria-sort", "ascending"));
    await user.click(screen.getByRole("button", { name: /Última conexión/ }));
    await waitFor(() => expect(lastList(calls)).toContain("sort=last_seen_at"));
  });

  it("carga los filtros desde la URL al abrir", async () => {
    const calls = setup();
    renderApp("/dispositivos?status=offline&platform=linux&q=srv&issue=low_disk");
    await screen.findByRole("link", { name: "PC-CONTA-07" });
    const p = lastList(calls);
    for (const want of ["status=offline", "platform=linux", "q=srv", "issue=low_disk"]) expect(p).toContain(want);
    expect(screen.getByLabelText("Estado")).toHaveValue("offline");
    expect(screen.getByRole("searchbox", { name: "Buscar" })).toHaveValue("srv");
    expect(screen.getByText(/Poco espacio en disco/)).toBeInTheDocument();
  });

  it("sin resultados muestra «Ningún dispositivo coincide con los filtros.» y un botón para quitarlos", async () => {
    const calls = setup([]);
    const { location } = renderApp("/dispositivos?platform=linux");
    expect(await screen.findByText("Ningún dispositivo coincide con los filtros.")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Quitar filtros" }));
    await waitFor(() => expect(location()).toBe("/dispositivos"));
    expect(lastList(calls)).not.toContain("platform");
  });

  it("sin dispositivos ni filtros muestra «Aún no hay dispositivos inscritos.» con enlace a Inscripción", async () => {
    setup([]);
    renderApp("/dispositivos");
    expect(await screen.findByText("Aún no hay dispositivos inscritos.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Crear un token de inscripción" })).toHaveAttribute("href", "/inscripcion");
  });

  it("el enlace de exportación lleva los filtros actuales", async () => {
    setup();
    renderApp("/dispositivos?platform=linux&status=online");
    const link = await screen.findByRole("link", { name: "Exportar CSV" });
    const href = link.getAttribute("href")!;
    expect(href).toMatch(/^\/api\/v1\/devices\.csv\?/);
    expect(href).toContain("platform=linux");
    expect(href).toContain("status=online");
  });

  it("pagina y muestra el total", async () => {
    const calls = setup([device()], 60);
    renderApp("/dispositivos");
    expect(await screen.findByText("1–25 de 60")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Siguiente" }));
    await waitFor(() => expect(lastList(calls)).toContain("page=2"));
    expect(screen.getByRole("button", { name: "Anterior" })).toBeEnabled();
  });

  it("refresca el listado cada 30 s", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const calls = setup();
    renderApp("/dispositivos");
    await screen.findByRole("link", { name: "PC-CONTA-07" });
    const before = calls.filter((c) => c.path.startsWith("/devices")).length;
    await vi.advanceTimersByTimeAsync(31_000);
    await waitFor(() => expect(calls.filter((c) => c.path.startsWith("/devices")).length).toBeGreaterThan(before));
  });

  it("la búsqueda se vacía al quitar los filtros desde el estado vacío", async () => {
    setup([]);
    renderApp("/dispositivos?q=nada");
    const box = await screen.findByRole("searchbox", { name: "Buscar" });
    expect(box).toHaveValue("nada");
    await userEvent.setup().click(await screen.findByRole("button", { name: "Quitar filtros" }));
    await waitFor(() => expect(box).toHaveValue(""));
  });

  it("la búsqueda sigue a la URL cuando cambia desde fuera", async () => {
    setup();
    renderApp("/dispositivos?q=uno");
    const box = await screen.findByRole("searchbox", { name: "Buscar" });
    await userEvent.setup().click(screen.getByRole("link", { name: "Dispositivos" }));
    await waitFor(() => expect(box).toHaveValue(""));
  });
});
