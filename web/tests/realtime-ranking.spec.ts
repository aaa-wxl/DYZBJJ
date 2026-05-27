import { expect, test, type Page } from "@playwright/test";

async function login(page: Page, username: string, password: string) {
  await page.getByLabel("用户名").fill(username);
  await page.getByLabel("密码").fill(password);
  await page.getByRole("button", { name: "进入" }).click();
}

test("JWT 多用户出价后两端实时刷新 Top 5，刷新后身份稳定", async ({ browser }) => {
  const productName = `星河手镯 ${Date.now()}`;
  const adminContext = await browser.newContext();
  const userAContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const userBContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const admin = await adminContext.newPage();
  const userA = await userAContext.newPage();
  const userB = await userBContext.newPage();

  await admin.goto("/admin");
  await login(admin, "admin", "admin123");
  await expect(admin.getByRole("heading", { name: "竞拍管理" })).toBeVisible();

  await admin.getByLabel("商品名称").fill(productName);
  await admin.getByLabel("起拍价").fill("0");
  await admin.getByLabel("加价幅度").fill("100");
  await admin.getByLabel("竞拍时长(秒)").fill("120");
  await admin.getByLabel("封顶价").fill("500");
  await admin.getByLabel("延时窗口(秒)").fill("20");
  await admin.getByLabel("延长时长(秒)").fill("30");
  await admin.getByRole("button", { name: "创建竞拍" }).click();
  await expect(admin.getByRole("button", { name: new RegExp(productName) })).toBeVisible();
  await admin.getByRole("button", { name: "启动" }).click();
  await expect(admin.getByText("RUNNING").first()).toBeVisible();

  await userA.goto("/m");
  await login(userA, "userA", "123456");
  await userA.getByRole("button", { name: new RegExp(productName) }).click();
  await expect(userA.getByRole("heading", { name: productName })).toBeVisible();
  await userA.locator(".bid-dock input").fill("100");
  await userA.getByRole("button", { name: "出价" }).click();
  await expect(userA.getByText("用户A").first()).toBeVisible();
  await expect(userA.getByText("¥100").first()).toBeVisible();

  await userB.goto("/m");
  await login(userB, "userB", "123456");
  await userB.getByRole("button", { name: new RegExp(productName) }).click();
  await userB.locator(".bid-dock input").fill("200");
  await userB.getByRole("button", { name: "出价" }).click();

  await expect(admin.getByText("用户B").first()).toBeVisible();
  await expect(admin.getByText("¥200").first()).toBeVisible();
  await expect(userA.getByText("用户B").first()).toBeVisible();
  await expect(userA.getByText("第 2")).toBeVisible();

  await userA.reload();
  await expect(userA.getByText("用户A").first()).toBeVisible();
  await expect(userA.getByText("第 2")).toBeVisible();

  await userB.locator(".bid-dock input").fill("500");
  await userB.getByRole("button", { name: "出价" }).click();
  await expect(admin.getByText("SOLD").first()).toBeVisible();
  await expect(userA.getByText("SOLD").first()).toBeVisible();

  await adminContext.close();
  await userAContext.close();
  await userBContext.close();
});
