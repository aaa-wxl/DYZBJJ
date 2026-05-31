import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  expect: {
    timeout: 8_000
  },
  use: {
    baseURL: "http://127.0.0.1:5174",
    trace: "retain-on-failure"
  },
  projects: [
    {
      name: "msedge",
      use: { ...devices["Desktop Edge"], channel: "msedge" }
    }
  ],
  webServer: [
    {
      command: "set HTTP_ADDR=127.0.0.1:18080&& set REDIS_ADDR=127.0.0.1:6379&& go run ../cmd/api",
      url: "http://127.0.0.1:18080/healthz",
      reuseExistingServer: false,
      timeout: 30_000
    },
    {
      command: "set VITE_API_BASE=http://127.0.0.1:18080&& npm run dev -- --host 127.0.0.1 --port 5174",
      url: "http://127.0.0.1:5174",
      reuseExistingServer: false,
      timeout: 30_000
    }
  ]
});
