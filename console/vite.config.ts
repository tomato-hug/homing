import { defineConfig } from "vite";

// 页面由 homingd 挂在 http://127.0.0.1:18787/console 下，
// 所以 base 必须是 /console/，否则 assets 会去找根路径。
export default defineConfig({
  base: "/console/",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    target: "es2022",
    assetsInlineLimit: 4096,
    rollupOptions: {
      output: {
        entryFileNames: "assets/[name]-[hash].js",
        chunkFileNames: "assets/[name]-[hash].js",
        assetFileNames: "assets/[name]-[hash][extname]",
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://127.0.0.1:18787", changeOrigin: false },
    },
  },
});
