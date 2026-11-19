import { describe, expect, it } from "vitest";
import { screen, within } from "@testing-library/react";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { me, summary } from "../test/fixtures";

describe("Resumen", () => {
  it("muestra totales por estado y por plataforma", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /summary": { body: summary() } });
    renderApp("/");
    expect(await screen.findByText("412")).toBeInTheDocument();
    const legend = screen.getByRole("list", { name: "Estado de los equipos" });
    expect(within(legend).getByRole("link", { name: /351.*En línea/ })).toBeInTheDocument();
    expect(within(legend).getByRole("link", { name: /6.*Revocado/ })).toBeInTheDocument();
    expect(screen.getByText(/301 Windows/)).toBeInTheDocument();
    expect(screen.getByText(/111 Linux/)).toBeInTheDocument();
  });

  it("la cifra «En línea» enlaza a /dispositivos?status=online", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /summary": { body: summary() } });
    renderApp("/");
    const legend = await screen.findByRole("list", { name: "Estado de los equipos" });
    expect(within(legend).getByRole("link", { name: /En línea/ })).toHaveAttribute("href", "/dispositivos?status=online");
  });

  it("«Poco espacio en disco», «Sin cifrar» y «Sin conexión reciente» muestran su cifra y enlazan al listado", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /summary": { body: summary() } });
    renderApp("/");
    const issues = await screen.findByRole("list", { name: "Pendiente de revisar" });
    expect(within(issues).getByRole("link", { name: /9.*Poco espacio en disco/ })).toHaveAttribute("href", "/dispositivos?issue=low_disk");
    expect(within(issues).getByRole("link", { name: /23.*Sin conexión reciente/ })).toHaveAttribute("href", "/dispositivos?issue=stale");
    // Con 0, no hay nada que revisar: sin enlace.
    expect(within(issues).queryByRole("link", { name: /Sin cifrar/ })).toBeNull();
    expect(within(issues).getByText(/Sin cifrar/)).toBeInTheDocument();
  });

  it("lista las versiones de sistema y las últimas inscripciones", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /summary": { body: summary() } });
    renderApp("/");
    expect(await screen.findByText("Windows 11 Pro 10.0.26100")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "PC-NUEVO-01" })).toHaveAttribute("href", "/dispositivos/d9");
  });

  it("sin equipos invita a inscribir el primero", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /summary": { body: summary({ total: 0, by_status: { online: 0, offline: 0, inactive: 0, revoked: 0 }, os_versions: [], recent_enrollments: [] }) } });
    renderApp("/");
    expect(await screen.findByText("Aún no hay dispositivos inscritos.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Crear un token de inscripción" })).toBeInTheDocument();
  });
});

describe("Resumen: software y parches", () => {
  it("muestra los equipos con parches de seguridad pendientes y con despliegues fallidos", async () => {
    mockApi({ "GET /auth/me": { body: me() }, "GET /summary": { body: summary({ security_updates_pending: 7, deployment_failures: 2 }) } });
    renderApp("/");
    const list = await screen.findByRole("list", { name: "Software y parches" });
    expect(within(list).getByText("7")).toBeInTheDocument();
    expect(within(list).getByRole("link", { name: /2 Con algún despliegue fallido/ })).toHaveAttribute("href", "/despliegues");
  });
});
