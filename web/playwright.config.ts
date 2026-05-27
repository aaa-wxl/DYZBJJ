import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  use: {
    baseURL: "http://127.0.0.1:5173",
    channel: "msedge",
    trace: "retain-on-failure"
  },
  projects: [
    {
      name: "edge",
      use: { ...devices["Desktop Edge"] }
    }
  ]
});
