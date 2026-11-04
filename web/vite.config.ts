import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "assets/dist",
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            { name: "terminal", test: /node_modules\/@xterm/ },
            {
              name: "react",
              test: /node_modules\/(react|react-dom|scheduler)\//,
            },
          ],
        },
      },
    },
  },
  server: { proxy: { "/api": { target: "http://127.0.0.1:8080", ws: true } } },
});
