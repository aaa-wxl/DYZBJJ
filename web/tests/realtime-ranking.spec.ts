import { expect, test } from "@playwright/test";

test("管理端启动竞拍，用户端封顶成交", async ({ browser }) => {
  const productName = `星河手镯 ${Date.now()}`;
  const adminContext = await browser.newContext();
  const bidderContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const admin = await adminContext.newPage();
  const bidder = await bidderContext.newPage();

  await admin.goto("/admin");
  await admin.getByRole("button", { name: "进入" }).click();
  await expect(admin.getByRole("heading", { name: "竞拍管理" })).toBeVisible();

  await admin.getByLabel("商品名称").fill(productName);
  await admin.getByLabel("起拍价").fill("0");
  await admin.getByLabel("加价幅度").fill("100");
  await admin.getByLabel("竞拍时长(秒)").fill("120");
  await admin.getByLabel("封顶价").fill("300");
  await admin.getByLabel("延时窗口(秒)").fill("20");
  await admin.getByLabel("延长时长(秒)").fill("30");
  await admin.getByRole("button", { name: "创建竞拍" }).click();
  await expect(admin.getByRole("button", { name: new RegExp(productName) })).toBeVisible();
  await admin.getByRole("button", { name: "启动" }).click();
  await expect(admin.getByText("RUNNING").first()).toBeVisible();

  await bidder.goto("/m");
  await bidder.getByRole("button", { name: "进入" }).click();
  await bidder.getByRole("button", { name: new RegExp(productName) }).click();
  await expect(bidder.getByRole("heading", { name: productName })).toBeVisible();
  await bidder.locator(".bid-dock input").fill("300");
  await bidder.getByRole("button", { name: "出价" }).click();

  await expect(bidder.getByText("SOLD").first()).toBeVisible();
  await expect(bidder.getByText(/成交价 ￥300|已成交/)).toBeVisible();

  await admin.getByRole("button", { name: "刷新" }).click();
  await expect(admin.getByText("SOLD").first()).toBeVisible();

  await adminContext.close();
  await bidderContext.close();
});
