import assert from "node:assert/strict";
import { access, readFile, readdir } from "node:fs/promises";
import test from "node:test";

const siteRoot = new URL("../", import.meta.url);
const docsRoot = new URL("../../docs/", import.meta.url);
const generated = JSON.parse(
  await readFile(new URL("../app/data/research.generated.json", import.meta.url), "utf8"),
);

async function render(pathname = "/") {
  const workerUrl = new URL("../dist/server/index.js", import.meta.url);
  workerUrl.searchParams.set("test", `${process.pid}-${Date.now()}-${pathname}`);
  const { default: worker } = await import(workerUrl.href);

  return worker.fetch(
    new Request(`http://localhost${pathname}`, {
      headers: { accept: "text/html", host: "localhost" },
    }),
    {
      ASSETS: {
        fetch: async () => new Response("Not found", { status: 404 }),
      },
    },
    {
      waitUntil() {},
      passThroughOnException() {},
    },
  );
}

test("server-renders the research index from repository documents", async () => {
  const response = await render();
  assert.equal(response.status, 200);
  assert.match(response.headers.get("content-type") ?? "", /^text\/html\b/i);

  const html = await response.text();
  assert.match(html, /<title>mini-loop Research Atlas<\/title>/i);
  assert.match(html, /把每次调研，变成可以继续探索的知识地图。/);
  assert.match(html, /Research index/i);
  assert.match(html, /搜索标题、结论、章节与关键词/);
  assert.match(html, /\/research\/longhorizon-harness-research/);
  assert.match(html, /\/og\.png/);
  assert.doesNotMatch(html, /codex-preview|react-loading-skeleton|SkeletonPreview/);
});

test("detail metadata and content come from the same generated record", async () => {
  for (const slug of [
    "longhorizon-harness-research",
    "openai-codex-harness-research",
    "hardening-notes",
  ]) {
    const document = generated.documents.find((candidate) => candidate.slug === slug);
    assert.ok(document);

    const response = await render(`/research/${document.slug}`);
    assert.equal(response.status, 200);
    const html = await response.text();
    const escapedTitle = document.title.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const fullTitle = `${escapedTitle} \\| mini-loop Research Atlas`;
    const escapedSummary = document.summary.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

    assert.match(html, new RegExp(`<title>${fullTitle}</title>`));
    assert.match(html, new RegExp(`name="description" content="${escapedSummary}"`));
    assert.match(html, new RegExp(`property="og:title" content="${fullTitle}"`));
    assert.match(html, new RegExp(`property="og:description" content="${escapedSummary}"`));
    assert.match(html, new RegExp(`name="twitter:title" content="${fullTitle}"`));
    assert.match(html, new RegExp(`name="twitter:description" content="${escapedSummary}"`));
    assert.match(
      html,
      new RegExp(`约\\s*(?:<!-- -->)?${document.readingMinutes}(?:<!-- -->)?\\s*分钟`),
    );
    assert.match(html, new RegExp(`href="#${document.sections[0].id}"`));
    assert.doesNotMatch(html, /og\.png|property="og:image"|name="twitter:image"/);
  }

  const longHorizon = generated.documents.find(
    (candidate) => candidate.slug === "longhorizon-harness-research",
  );
  const response = await render(`/research/${longHorizon.slug}`);
  const html = await response.text();
  assert.match(html, /类型化已验证检查点/);
  assert.match(html, /\/research\/token-efficiency-components/);
  assert.match(html, /github\.com\/AMAP-ML\/LongHorizon-Harness\/blob/);
  assert.match(
    html,
    new RegExp(`github\\.com/luoyjx/mini-loop/blob/${longHorizon.sourceCommit}/mini_loop/harness\\.py`),
  );
  assert.doesNotMatch(html, /\/research\/readme-zh-cn/);

  const codexHarness = generated.documents.find(
    (candidate) => candidate.slug === "openai-codex-harness-research",
  );
  const codexResponse = await render("/research/" + codexHarness.slug);
  const codexHtml = await codexResponse.text();
  assert.match(codexHtml, /OpenAI Codex Harness 源码级调研/);
  assert.match(codexHtml, /<figcaption>Mermaid diagram source<\/figcaption>/);
  assert.match(
    codexHtml,
    /github\.com\/openai\/codex\/blob\/758ef40f50c1a458425c7cfbf1eb12cbc07af0b0\/codex-rs\/core\/src\/tools\/spec_plan\.rs#L121-L176/,
  );
  assert.match(codexHtml, /openai\.com\/index\/unlocking-the-codex-harness/);
  assert.doesNotMatch(codexHtml, /\]\[src-|~~~mermaid/);
});

test("generated content covers every top-level docs markdown file", async () => {
  const expected = (await readdir(docsRoot))
    .filter((filename) => filename.endsWith(".md") && !["AGENTS.md", "README.md"].includes(filename))
    .map((filename) => `docs/${filename}`)
    .sort();
  const actual = generated.documents.map((document) => document.sourcePath).sort();

  assert.deepEqual(actual, expected);
  assert.equal(new Set(generated.documents.map((document) => document.slug)).size, expected.length);
  for (const document of generated.documents) {
    assert.ok(document.title.length > 2);
    assert.ok(document.summary.length > 8);
    assert.ok(document.sections.length > 0);
    assert.ok(document.readingMinutes > 0);
  }
});

test("removes starter-only preview assets and dependency", async () => {
  const packageJson = await readFile(new URL("../package.json", import.meta.url), "utf8");
  assert.doesNotMatch(packageJson, /react-loading-skeleton|site-creator-vinext-starter/);
  await assert.rejects(access(new URL("../app/_sites-preview", import.meta.url)));
  await access(new URL("../public/og.png", import.meta.url));
  await access(new URL("../scripts/build-research.mjs", import.meta.url));
  await access(new URL(".openai/hosting.json", siteRoot));
});

test("uses native document navigation instead of the vinext Link runtime", async () => {
  const navigationSources = await Promise.all([
    readFile(new URL("../app/research-index.tsx", import.meta.url), "utf8"),
    readFile(new URL("../app/site-header.tsx", import.meta.url), "utf8"),
    readFile(new URL("../app/research/[slug]/page.tsx", import.meta.url), "utf8"),
  ]);

  for (const source of navigationSources) {
    assert.doesNotMatch(source, /from ["']next\/link["']/);
  }
});
