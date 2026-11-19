import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import Upload from "./Upload";

// XMLHttpRequest simulado que permite controlar el progreso.
class FakeXHR {
  static last: FakeXHR | null = null;
  upload = { onprogress: null as ((e: { loaded: number; total: number; lengthComputable: boolean }) => void) | null };
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;
  status = 0;
  responseText = "";
  headers: Record<string, string> = {};
  method = "";
  url = "";
  aborted = false;
  body: FormData | null = null;
  constructor() { FakeXHR.last = this; }
  open(m: string, u: string) { this.method = m; this.url = u; }
  setRequestHeader(k: string, v: string) { this.headers[k] = v; }
  send(b: FormData) { this.body = b; }
  abort() { this.aborted = true; this.onabort?.(); }
}

afterEach(() => vi.unstubAllGlobals());

describe("Upload", () => {
  it("muestra el progreso y, al terminar, entrega el SHA-256", async () => {
    vi.stubGlobal("XMLHttpRequest", FakeXHR);
    const done = vi.fn();
    render(<Upload onUploaded={done} />);
    const file = new File(["contenido"], "Conta 2026.msi", { type: "application/x-msi" });
    await userEvent.setup().upload(screen.getByLabelText("Instalador"), file);
    const xhr = FakeXHR.last!;
    expect(xhr.method).toBe("POST");
    expect(xhr.url).toBe("/api/v1/files");
    expect(xhr.body!.get("file")).toBeInstanceOf(File);
    xhr.upload.onprogress!({ loaded: 40, total: 100, lengthComputable: true });
    expect(await screen.findByText("40 %")).toBeInTheDocument();
    xhr.status = 201;
    xhr.responseText = JSON.stringify({ sha256: "a".repeat(64), size: 9, original_name: "Conta 2026.msi" });
    xhr.onload!();
    await waitFor(() => expect(done).toHaveBeenCalledWith({ sha256: "a".repeat(64), size: 9, original_name: "Conta 2026.msi" }));
  });

  it("cancelar la subida aborta la petición", async () => {
    vi.stubGlobal("XMLHttpRequest", FakeXHR);
    render(<Upload onUploaded={vi.fn()} />);
    const user = userEvent.setup();
    await user.upload(screen.getByLabelText("Instalador"), new File(["x"], "a.msi"));
    await user.click(screen.getByRole("button", { name: "Cancelar subida" }));
    expect(FakeXHR.last!.aborted).toBe(true);
    expect(await screen.findByText("Subida cancelada.")).toBeInTheDocument();
  });

  it("muestra el error del servidor", async () => {
    vi.stubGlobal("XMLHttpRequest", FakeXHR);
    render(<Upload onUploaded={vi.fn()} />);
    await userEvent.setup().upload(screen.getByLabelText("Instalador"), new File(["x"], "a.msi"));
    const xhr = FakeXHR.last!;
    xhr.status = 507;
    xhr.responseText = JSON.stringify({ error: { code: "unavailable", message: "No queda espacio en el disco del servidor (se reserva 1 GB libre)." } });
    xhr.onload!();
    expect(await screen.findByText(/No queda espacio/)).toBeInTheDocument();
  });
});
