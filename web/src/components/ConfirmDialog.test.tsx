import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import ConfirmDialog from "./ConfirmDialog";

function Harness({ onConfirm }: { onConfirm: () => void }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button onClick={() => setOpen(true)}>Abrir</button>
      {open && (
        <ConfirmDialog title="Eliminar este dispositivo" body="Se borrarán sus datos." confirmLabel="Eliminar" danger requireText="PC-1"
          onConfirm={onConfirm} onCancel={() => setOpen(false)} />
      )}
    </>
  );
}

describe("ConfirmDialog", () => {
  it("con requireText, el botón queda deshabilitado hasta escribir el texto exacto", async () => {
    const onConfirm = vi.fn();
    render(<Harness onConfirm={onConfirm} />);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Abrir" }));
    const btn = screen.getByRole("button", { name: "Eliminar" });
    expect(btn).toBeDisabled();
    await user.type(screen.getByRole("textbox"), "pc-1");
    expect(btn).toBeDisabled();
    await user.clear(screen.getByRole("textbox"));
    await user.type(screen.getByRole("textbox"), "PC-1");
    await user.click(btn);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("Escape cancela y el foco vuelve al elemento que abrió el diálogo", async () => {
    render(<Harness onConfirm={vi.fn()} />);
    const user = userEvent.setup();
    const opener = screen.getByRole("button", { name: "Abrir" });
    await user.click(opener);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(opener).toHaveFocus();
  });
});
