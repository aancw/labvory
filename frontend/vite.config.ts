import path from "node:path"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    // Bind all interfaces so the dev server is reachable from other devices.
    host: true,
    proxy: {
      // Override with LV_API_TARGET when the API runs on a non-default port.
      // 127.0.0.1 (not localhost): avoids IPv6/IPv4 ambiguity when other tools
      // hold loopback binds on the same port.
      "/api": { target: process.env.LV_API_TARGET ?? "http://127.0.0.1:8090", changeOrigin: false },
      "/healthz": { target: process.env.LV_API_TARGET ?? "http://127.0.0.1:8090", changeOrigin: false },
    },
  },
  // `vite preview` serves the production build (used by `make run`); it needs the
  // same proxy so the built SPA can reach the local API.
  preview: {
    proxy: {
      "/api": { target: process.env.LV_API_TARGET ?? "http://127.0.0.1:8090", changeOrigin: false },
      "/healthz": { target: process.env.LV_API_TARGET ?? "http://127.0.0.1:8090", changeOrigin: false },
    },
  },
})
