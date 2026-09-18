// Only disposable fixtures supplied by TestR3BrowserJourney are used here.
// No public relay, production credentials, real addresses or DNS changes.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const { chromium, expect } = require(
  process.env.TABMAIL_E2E_BROWSER_MODULE || "@playwright/test",
);
const fixture = JSON.parse(process.env.TABMAIL_BROWSER_FIXTURE || "{}");
const origin = process.env.TABMAIL_E2E_ORIGIN || "http://localhost:3000";

(async () => {
  const browser = await chromium.launch({
    headless: true,
    executablePath: process.env.TABMAIL_CHROMIUM_PATH || undefined,
  });
  const context = await browser.newContext();
  await context.addInitScript(() =>
    localStorage.setItem("tabmail-locale", "en"),
  );
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  page.on("dialog", (dialog) => dialog.accept());
  const errors = [];
  let legacySendRequests = 0;
  page.on("request", (request) => {
    if (request.method() === "POST" && new URL(request.url()).pathname === "/api/v1/send") {
      legacySendRequests++;
    }
  });
  page.on("pageerror", (error) => errors.push(String(error)));
  async function login(email) {
    await page.goto(origin);
    await page.getByLabel("Login email", { exact: true }).fill(email);
    await page.getByLabel("Password", { exact: true }).fill(fixture.password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Company mail", exact: true }),
    ).toBeVisible();
  }
  async function logout() {
    await page.goto(origin);
    await page.getByRole("button", { name: "Sign out", exact: true }).click();
    await expect(page.getByLabel("Login email", { exact: true })).toBeVisible();
  }
  try {
    await login(fixture.employee);
    await page.getByRole("button", { name: /Browser welcome/ }).click();
    await expect(
      page.getByText("Private browser journey body.", { exact: false }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Reply", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "Compose mail", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByLabel("To (plain email addresses)", { exact: true }),
    ).toHaveValue("client@recipient.test");
    const save = page.waitForResponse(
      (r) =>
        r.url().includes("/company/drafts") && r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Save draft", exact: true }).click();
    assert.equal((await save).status(), 200);
    await page.getByRole("button", { name: "Close", exact: true }).click();
    await page.getByRole("button", { name: "Drafts", exact: true }).click();
    await page
      .getByRole("button", { name: "Continue editing", exact: true })
      .click();
    const upload = page.waitForResponse(
      (r) =>
        r.url().includes("/attachments") && r.request().method() === "POST",
    );
    await page
      .getByLabel("Attachments (10 files, 20 MiB total)", { exact: true })
      .setInputFiles({
        name: "browser.txt",
        mimeType: "text/plain",
        buffer: Buffer.from("Browser attachment bytes"),
      });
    assert.equal((await upload).status(), 200);
    await expect(
      page.getByText("browser.txt", { exact: false }).first(),
    ).toBeVisible();
    const send = page.waitForResponse(
      (r) =>
        /^\/api\/v1\/company\/drafts\/[0-9a-f-]{36}\/submit$/.test(new URL(r.url()).pathname) &&
        r.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Send", exact: true }).click();
    const submitted = await send;
    assert.equal(submitted.status(), 201);
    assert.ok(submitted.request().headers()["idempotency-key"]);
    assert.ok(submitted.request().postDataJSON().expected_revision >= 1);
    assert.equal(legacySendRequests, 0, "retired send endpoint must never be used");
    const submittedID = (await submitted.json()).data.id;
    assert.ok(submittedID, "submission must return a durable identity");
    const sentRow = page.getByRole("button", { name: /Re: Browser welcome/ });
    await expect(sentRow).toBeVisible();
    await sentRow.click();
    const sentContent = page.waitForResponse((response) =>
      new URL(response.url()).pathname === `/api/v1/company/submissions/${submittedID}/content`,
    );
    await page.getByRole("button", { name: "View content", exact: true }).click();
    assert.equal((await sentContent).status(), 200);
    await expect(page.getByText("Private browser journey body.", { exact: false })).toBeVisible();
    const downloadReady = page.waitForEvent("download");
    await page.getByRole("button", { name: /browser\.txt/ }).click();
    const download = await downloadReady;
    assert.equal(download.suggestedFilename(), "browser.txt");
    const reader = await download.createReadStream();
    assert.ok(reader, "sent attachment must be downloadable");
    const chunks = [];
    for await (const chunk of reader) chunks.push(chunk);
    assert.equal(Buffer.concat(chunks).toString(), "Browser attachment bytes");
    console.log(
      "PASS: employee read -> RFC reply -> draft -> attachment -> atomic submit -> sent content and verified download",
    );

    await logout();
    await login(fixture.other);
    await expect(page.getByLabel("Current mailbox")).toContainText(
      "successor@company.test",
    );
    await expect(
      page.getByText("Private browser journey body.", { exact: false }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: /Browser welcome/ }),
    ).toHaveCount(0);
    console.log(
      "PASS: account switch clears old mailbox, body and draft state",
    );

    // Two pages share a refresh cookie, but must serialize its one-time rotation.
    await page.evaluate(
      (token) => localStorage.setItem("tabmail_access_token", token),
      fixture.expired_token,
    );
    const second = await context.newPage();
    await Promise.all([page.reload(), second.goto(origin + "/mail")]);
    for (const tab of [page, second]) {
      await expect(tab.getByLabel("Current mailbox")).toContainText(
        "successor@company.test",
      );
    }
    await second.close();
    console.log("PASS: concurrent real browser tabs recover an expired token");

    await logout();
    await login(fixture.admin);
    await page.goto(origin + "/company");
    await page
      .getByLabel("Login email", { exact: true })
      .fill("browser-new@contact.test");
    await page
      .getByLabel("Display name", { exact: true })
      .fill("Browser New Employee");
    await page
      .getByLabel("Company mailbox local part", { exact: true })
      .fill("browser-new");
    await page
      .getByRole("button", { name: "Create employee invitation", exact: true })
      .click();
    const activation = page.getByLabel(
      "One-time activation link (not retained after leaving)",
      { exact: true },
    );
    await expect(activation).toHaveValue(/\/auth\/activate#[a-f0-9]{64}$/);
    const url = await activation.inputValue();
    const employeeContext = await browser.newContext();
    await employeeContext.addInitScript(() =>
      localStorage.setItem("tabmail-locale", "en"),
    );
    const activate = await employeeContext.newPage();
    await activate.goto(url);
    await activate
      .getByLabel("New password", { exact: true })
      .fill(fixture.password);
    await activate
      .getByLabel("Confirm password", { exact: true })
      .fill(fixture.password);
    await activate
      .getByRole("button", {
        name: "Activate and provision mailbox",
        exact: true,
      })
      .click();
    await expect(
      activate.getByText("Your account and personal mailbox are ready.", {
        exact: false,
      }),
    ).toBeVisible();
    await employeeContext.close();
    await page.goto(origin + "/company/templates");
    await expect(
      page.getByRole("heading", { name: /template/i }).first(),
    ).toBeVisible();
    console.log(
      "PASS: administrator invitation -> isolated employee activation -> template console",
    );
    assert.deepEqual(errors, [], "browser runtime errors");
  } catch (error) {
    const dir =
      process.env.TABMAIL_BROWSER_EVIDENCE || "/tmp/tabmail-browser-evidence";
    fs.mkdirSync(dir, { recursive: true });
    await page
      .screenshot({ path: dir + "/failure.png", fullPage: true })
      .catch(() => {});
    fs.writeFileSync(
      dir + "/failure.html",
      await page.content().catch(() => ""),
    );
    fs.writeFileSync(
      dir + "/errors.txt",
      String(error) + "\n" + errors.join("\n"),
    );
    throw error;
  } finally {
    await context.close();
    await browser.close();
  }
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
