import { render } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { AppProviders, AppRoutes } from "../App";

/** Monta la aplicación completa en la ruta dada y permite leer la ruta actual. */
export function renderApp(path: string) {
  let current = path;
  function Spy() {
    const l = useLocation();
    current = l.pathname + l.search;
    return null;
  }
  const utils = render(
    <MemoryRouter initialEntries={[path]}>
      <AppProviders>
        <Spy />
        <AppRoutes />
      </AppProviders>
    </MemoryRouter>,
  );
  return { ...utils, location: () => current };
}
