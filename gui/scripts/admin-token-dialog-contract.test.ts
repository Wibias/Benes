import assert from "node:assert/strict";
import test from "node:test";

import { catalogValue } from "../src/i18n/catalogs.ts";
import { promptForAdminToken } from "../src/admin-token-dialog.ts";
import { FakeElement, installGuiDom } from "./gui-dom-harness.ts";

function event(type: string): { type: string; preventDefault(): void; cancelable: boolean } {
  return { type, cancelable: true, preventDefault() {} };
}

test("admin token dialog is a password-manager sign-in form with Benes account semantics", async () => {
  const dom = installGuiDom();
  try {
    const pending = promptForAdminToken(async () => "accepted", "en");
    const dialog = dom.document.querySelector("#benes-admin-token-dialog") as FakeElement;
    const form = dialog.querySelector("form") as FakeElement;
    const username = form.elements.namedItem("username") as FakeElement;
    const password = form.elements.namedItem("password") as FakeElement;

    assert.ok(dialog);
    assert.equal(dialog.getAttribute("aria-labelledby"), "benes-admin-token-dialog-title");
    assert.equal(dialog.querySelector("h3")?.textContent, catalogValue("en", "auth.adminTokenTitle"));
    assert.equal(form.method, "post");
    assert.equal(form.autocomplete, "on");
    assert.equal(form.action, "http://127.0.0.1:23100/");
    assert.equal(username.id, "benes-admin-token-dialog-username");
    assert.equal(form.querySelector(`label[for="${username.id}"]`)?.textContent, catalogValue("en", "auth.adminAccountLabel"));
    assert.equal(username.autocomplete, "username");
    assert.equal(username.readOnly, true);
    assert.equal(username.value, "Benes");
    assert.equal(password.id, "benes-admin-token-dialog-password");
    assert.equal(password.type, "password");
    assert.equal(password.name, "password");
    assert.equal(password.autocomplete, "current-password");
    assert.equal(password.required, true);
    assert.equal(password.spellcheck, false);
    assert.equal(password.autocapitalize, "none");

    password.value = "  secret-token  ";
    form.dispatchEvent(event("submit"));
    assert.equal(await pending, "secret-token");
    assert.equal(dom.document.querySelector("#benes-admin-token-dialog"), null);
  } finally {
    dom.restore();
  }
});

test("blank submit does not resolve; reject and unavailable stay open; cancel restores focus", async () => {
  const dom = installGuiDom();
  try {
    const focusTarget = dom.document.createElement("button");
    dom.document.body.append(focusTarget);
    focusTarget.focus();

    const attempts: string[] = [];
    const pending = promptForAdminToken(async (token) => {
      attempts.push(token);
      if (token === "good") return "accepted";
      return "rejected";
    }, "en");

    const dialog = dom.document.querySelector("#benes-admin-token-dialog") as FakeElement;
    const form = dialog.querySelector("form") as FakeElement;
    const password = form.elements.namedItem("password") as FakeElement;

    password.value = "   ";
    form.dispatchEvent(event("submit"));
    await Promise.resolve();
    assert.deepEqual(attempts, []);
    assert.ok(dialog.parent);

    password.value = "bad";
    form.dispatchEvent(event("submit"));
    await Promise.resolve();
    await Promise.resolve();
    assert.deepEqual(attempts, ["bad"]);
    assert.equal(dialog.querySelector('[role="alert"]')?.textContent, catalogValue("en", "auth.adminTokenRejected"));
    assert.equal(password.value, "");
    assert.equal(password.disabled, false);

    const cancel = pending;
    dialog.dispatchEvent(event("cancel"));
    assert.equal(await cancel, null);
    assert.equal(dom.document.activeElement, focusTarget);
  } finally {
    dom.restore();
  }
});

test("verifier throw uses unavailable copy and cannot settle twice", async () => {
  const dom = installGuiDom();
  try {
    const pending = promptForAdminToken(async () => {
      throw new Error("offline");
    }, "en");
    const dialog = dom.document.querySelector("#benes-admin-token-dialog") as FakeElement;
    const form = dialog.querySelector("form") as FakeElement;
    const password = form.elements.namedItem("password") as FakeElement;
    password.value = "token";
    form.dispatchEvent(event("submit"));
    await Promise.resolve();
    await Promise.resolve();
    assert.equal(dialog.querySelector('[role="alert"]')?.textContent, catalogValue("en", "auth.adminTokenUnavailable"));
    dialog.dispatchEvent(event("cancel"));
    assert.equal(await pending, null);
  } finally {
    dom.restore();
  }
});
