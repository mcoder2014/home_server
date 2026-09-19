# Manual Preview UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add visual previews to manual upload/reorder cards, embed PDF reading in the detail page, and reduce the visual emphasis of item numbers and filenames.

**Architecture:** Keep the existing manuals API and data model unchanged. `ManualEditor.vue` owns short-lived local `blob:` preview URLs and reuses persisted `thumbnail_url` values; `ManualDetail.vue` uses the authenticated `content_url` in the browser-native PDF viewer and retains explicit open/download fallbacks.

**Tech Stack:** Vue 3 Options API, Element Plus, native object URLs and PDF iframe support, Node test runner, Playwright Chromium.

---

### Task 1: Specify editor previews and PDF reading behavior

**Files:**
- Modify: `front_vue/test/manuals-browser.cjs`

- [x] **Step 1: Add a failing editor browser test**

Add a test that opens `/manuals/500/edit?as=owner` with saved image and PDF items, selects one local PNG and one local PDF, then asserts:

```js
assert.equal(await page.locator('.queue-preview-image').count(), 1)
assert.equal(await page.locator('.queue-preview-pdf').count(), 1)
assert.equal(await page.locator('.saved-thumbnail').count(), 2)
await page.locator('.upload-queue-item').filter({hasText: 'local.pdf'}).getByRole('button', {name: '上移'}).click()
assert.match(await page.locator('.upload-queue-item').first().innerText(), /local\.pdf/)
```

- [x] **Step 2: Extend the detail browser test and verify RED**

Assert that the PDF item contains one `iframe.pdf-viewer`, its `src` is the protected `content_url`, the metadata uses `.item-meta`, and the former circular index selector is absent.

Run:

```bash
NODE_PATH=/Users/bytedance/go/src/github.com/mcoder2014/home_server/front_vue/node_modules node --test test/manuals-browser.cjs
```

Expected: FAIL because the preview and viewer selectors do not exist.

### Task 2: Implement editor thumbnails with bounded object URL lifetime

**Files:**
- Modify: `front_vue/src/views/ManualEditor.vue`
- Test: `front_vue/test/manuals-browser.cjs`

- [x] **Step 1: Render previews without changing queue semantics**

Add a preview block before each queue item's fields. Local images use an `img`; local PDFs use a lazy, non-interactive iframe. Saved image/PDF items use `thumbnail_url`; missing thumbnails render a type placeholder. Text and URL items keep compact type placeholders.

- [x] **Step 2: Manage local object URLs**

After `appendFileItems`, assign an object URL only to accepted image/PDF queue items. Revoke it when an item is removed, after a fully successful upload clears the queue, and in `beforeUnmount`. Moving an item must preserve its URL and identity.

- [x] **Step 3: Add responsive styling**

Use a fixed thumbnail column on desktop and a full-width preview above metadata below 640 px. Preview failures must not disable move, remove, upload, cover selection, or delete actions.

- [x] **Step 4: Run the browser test and verify GREEN**

Run the Task 1 command. Expected: all manuals browser tests pass.

### Task 3: Embed PDFs and quiet item metadata

**Files:**
- Modify: `front_vue/src/views/ManualDetail.vue`
- Test: `front_vue/test/manuals-browser.cjs`

- [x] **Step 1: Replace the prominent item heading**

Render `资料 N · 类型` as muted metadata. Render an explicit custom title as a modest heading and show `original_name` as small secondary text, avoiding duplicate title/filename output.

- [x] **Step 2: Add the native PDF viewer**

Render:

```vue
<iframe class="pdf-viewer" :src="item.content_url" :title="pdfViewerTitle(item)" loading="lazy"></iframe>
```

Keep the existing new-window and download links directly below it. Preserve the preview-unavailable notice and mobile fallback layout.

- [x] **Step 3: Run focused tests and verify GREEN**

Run the Task 1 command. Expected: all manuals browser tests pass, including mobile width and link-safety assertions.

### Task 4: Verify, document, review, and publish

**Files:**
- Modify: `docs/specs/manual-management/implementation.md`
- Modify: `docs/specs/manual-management/verification.md`

- [x] **Step 1: Record final behavior and verification evidence**

Update the existing implementation and verification documents with the editor thumbnails, embedded PDF reader, metadata hierarchy, exact commands, and observed results.

- [x] **Step 2: Run the full frontend verification**

Run from `front_vue` with the main checkout's dependency directory on `NODE_PATH`/`PATH`:

```bash
npm run test:frontend
npm run test:manuals-browser
npm run lint
npm run build
```

Expected: zero failures and a successful production build.

- [ ] **Step 3: Commit and dispatch independent review**

Commit the implementation, then give the review agent the base SHA, head SHA, requirements, design document, and validation results. Fix every blocking or important finding and rerun the affected checks.

- [ ] **Step 4: Create and merge the PR**

Push `feat/cq/manuals_ui`, create a PR describing the visible behavior and tests, wait for GitHub checks, and merge only if the independent review and required checks are clean.

- [ ] **Step 5: Deploy the merged commit to pi**

Build and stage the exact merged commit using the existing release-directory process, retain the previous release for rollback, switch `current`, restart services, and verify the embedded PDF, editor previews, permissions, `/ping`, and existing modules before removing the feature worktree.
