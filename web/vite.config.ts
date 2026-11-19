/// <reference types="vitest" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { writeFileSync } from "node:fs";

// dist/.gitkeep está en el repositorio para que el servidor compile sin la
// consola (go:embed necesita que la carpeta exista). Vite vacía dist al
// compilar, así que se vuelve a crear.
const keepDist = {
  name: "keep-dist",
  closeBundle() {
    writeFileSync("dist/.gitkeep", "");
  },
};

export default defineConfig({
  plugins: [react(), keepDist],
  server: {
    // En desarrollo, la API la sirve un mdm-server local detrás de TLS.
    proxy: { "/api": { target: "https://localhost:8443", secure: false } },
  },
  build: { outDir: "dist", emptyOutDir: true, sourcemap: false },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["src/test/setup.ts"],
    css: false,
  },
});
