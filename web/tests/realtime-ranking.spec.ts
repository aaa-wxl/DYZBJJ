import { expect, test, type Page } from "@playwright/test";

const apiBase = "http://127.0.0.1:8080";

test("两个用户实时看到各自视角的排名", async ({ browser, request }) => {
  const name = `PW实时排名-${Date.now()}`;
  const created = await request.post(`${apiBase}/api/auctions`, {
    data: {
      merchantId: "merchant-playwright",
      productName: name,
      imageUrl: "https://example.com/item.png",
      description: "Playwright 实时排名验证商品",
      startPrice: 0,
      increment: 100,
      durationSeconds: 60,
      ceilingPrice: 1000,
      extendThresholdSeconds: 0,
      extendBySeconds: 0
    }
  });
  expect(created.ok()).toBeTruthy();
  const auction = await created.json();

  const started = await request.post(`${apiBase}/api/auctions/${auction.id}/start`);
  expect(started.ok()).toBeTruthy();

  const userOne = await browser.newPage();
  const userTwo = await browser.newPage();
  await joinAuction(userOne, name, "user-1");
  await joinAuction(userTwo, name, "user-2");

  await placeBid(userOne, 100);
  await expect(rankValue(userOne)).toHaveText("#1");
  await expect(rankValue(userTwo)).toHaveText("-");

  await placeBid(userTwo, 200);
  await expect(rankValue(userOne)).toHaveText("#2");
  await expect(rankValue(userTwo)).toHaveText("#1");

  await userOne.close();
  await userTwo.close();
});

test("操作按钮点击后给出处理中和成功反馈", async ({ page }) => {
  await page.goto("/");
  await page.locator("label").filter({ hasText: "商品名称" }).locator("input").fill(`反馈验证-${Date.now()}`);
  await page.getByRole("button", { name: "创建竞拍" }).click();

  await expect(page.getByRole("button", { name: "创建中..." })).toBeVisible();
  await expect(page.getByRole("status")).toContainText("竞拍创建成功");
});

async function joinAuction(page: Page, productName: string, userId: string) {
  await page.goto("/");
  await page.getByRole("button", { name: new RegExp(productName) }).click();
  await page.locator(".bidline input").first().fill(userId);
  await expect(minimumBidValue(page)).toHaveText("¥100");
}

async function placeBid(page: Page, amount: number) {
  await page.locator(".bidline input").nth(1).fill(String(amount));
  await page.getByRole("button", { name: "立即出价" }).click();
}

function rankValue(page: Page) {
  return page.locator(".metric").filter({ hasText: "我的排名" }).locator("strong");
}

function minimumBidValue(page: Page) {
  return page.locator(".metric").filter({ hasText: "最低出价" }).locator("strong");
}
