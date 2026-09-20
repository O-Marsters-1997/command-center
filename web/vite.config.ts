import { defineConfig } from "vite";
import solid from "vite-plugin-solid";

// Output rides the same internal/cc/assets/dist that the tailwindcss build already writes
// app.css into (Justfile's assets target, //go:embed all: in server.go): one directory, one
// place Go looks. emptyOutDir stays false so this build never deletes app.css or .gitkeep.
export default defineConfig({
  plugins: [solid()],
  build: {
    outDir: "../internal/cc/assets/dist",
    emptyOutDir: false,
    lib: {
      entry: { graph: "src/graph.tsx", "launch-modal": "src/launch-modal.tsx", insights: "src/insights.tsx" },
      formats: ["es"],
      fileName: (_format, entryName) => `${entryName}.js`,
    },
  },
});
