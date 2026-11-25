import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import TempAdminBanner from "./TempAdminBanner";
import { mockApi } from "../test/mockApi";
import { AppProviders } from "../App";
import { me } from "../test/fixtures";

describe("TempAdminBanner", () => {
  it("avisa de los permisos de administrador temporal vigentes", async () => {
    const soon = new Date(Date.now() - 5 * 60_000).toISOString();
    mockApi({
      "GET /auth/me": { body: me() },
      "GET /devices/d1/commands": { body: [
        { id: "c1", device_id: "d1", hostname: "pc", batch_id: null, type: "temp_admin", params: { user: "EMPRESA\\ana", minutes: 60, reason: "Driver" },
          status: "succeeded", exit_code: null, error: "", created_at: soon, finished_at: soon },
      ] },
    });
    render(<MemoryRouter><AppProviders><TempAdminBanner deviceId="d1" hostname="pc" /></AppProviders></MemoryRouter>);
    expect(await screen.findByText(/EMPRESA\\ana es administrador temporal/)).toBeInTheDocument();
    expect(screen.getByText(/Driver/)).toBeInTheDocument();
  });
});
