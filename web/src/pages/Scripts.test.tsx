import { describe, expect, it } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mockApi } from "../test/mockApi";
import { renderApp } from "../test/render";
import { me } from "../test/fixtures";

const script = { id: "s1", name: "Limpiar", description: "", platform: "linux", interpreter: "bash", timeout_s: 300, latest_version: 2,
  archived: false, parameters: [], created_at: "", updated_at: "" };

describe("Scripts", () => {
  it("lista los scripts; un operador no puede crear", async () => {
    mockApi({ "GET /auth/me": { body: me("operator") }, "GET /scripts": { body: [script] } });
    renderApp("/scripts");
    const row = within((await screen.findByText("Limpiar")).closest("tr")!);
    expect(row.getByText("v2")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Nuevo script" })).toBeNull();
  });

  it("guardar el contenido crea una versión nueva y conserva el historial", async () => {
    const calls = mockApi({
      "GET /auth/me": { body: me() }, "GET /scripts/s1": { body: script },
      "GET /scripts/s1/versions": { body: [{ version: 2, created_by_email: "ana@ejemplo.es", created_at: "2026-11-16T10:00:00Z" }, { version: 1, created_by_email: "ana@ejemplo.es", created_at: "2026-11-15T10:00:00Z" }] },
      "GET /scripts/s1/versions/2": { body: { version: 2, content: "rm -rf /tmp/mdm-*", created_by_email: "", created_at: "" } },
      "POST /scripts/s1/versions": { status: 201, body: { version: 3 } },
      "PATCH /scripts/s1": { body: script },
    });
    renderApp("/scripts/s1");
    const user = userEvent.setup();
    const editor = await screen.findByLabelText("Contenido");
    await waitFor(() => expect(editor).toHaveValue("rm -rf /tmp/mdm-*"));
    await user.type(editor, "\necho hecho");
    await user.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ content: "rm -rf /tmp/mdm-*\necho hecho" }));
    expect(screen.getByText("Versión 1")).toBeInTheDocument();
  });
});
