import { defineConfig } from "vite";
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
export default defineConfig({
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../internal/proxy/web",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        entryFileNames: "dashboard.js",
        chunkFileNames: "chunk-[hash].js",
        assetFileNames: (asset) =>
          asset.names?.some((n) => n.endsWith(".css"))
            ? "dashboard.css"
            : "[name][extname]",
      },
    },
  },
});
