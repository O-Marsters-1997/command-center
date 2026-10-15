import { defineConfig } from "vite";
import solid from "vite-plugin-solid";

export default defineConfig({
  plugins: [solid()],
  build: {
    outDir: "../internal/web/assets/dist",
    emptyOutDir: false,
    minify: "esbuild",
    lib: {
      entry: { graph: "src/graph.tsx", "launch-modal": "src/launch-modal.tsx", keys: "src/keys.ts", glue: "src/glue.ts" },
      formats: ["es"],
      fileName: (_format, entryName) => `${entryName}.js`,
    },
    rollupOptions: { output: { entryFileNames: "[name].js", chunkFileNames: "[name].js" } },
  },
});
