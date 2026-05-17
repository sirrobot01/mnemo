import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The UI is served from the embedded Go binary at /, and proxies /v1 to the
// Mnemo API during local development.
export default defineConfig({
  plugins: [react()],
  base: "/",
  server: {
    port: 5173,
    proxy: {
      "/v1": "http://127.0.0.1:47321",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
