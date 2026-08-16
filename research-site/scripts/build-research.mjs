import { spawnSync } from "node:child_process";
import { mkdir, readFile, readdir, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const siteRoot = path.resolve(scriptDirectory, "..");
const repositoryRoot = path.resolve(siteRoot, "..");
const docsRoot = path.join(repositoryRoot, "docs");
const outputPath = path.join(siteRoot, "app", "data", "research.generated.json");
const excludedDocuments = new Set(["AGENTS.md", "README.md"]);

function plainText(markdown) {
  return markdown
    .replace(/<!--([\s\S]*?)-->/g, " ")
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/<https?:\/\/[^>]+>/g, " ")
    .replace(/[`*_~>#|]/g, " ")
    .replace(/^\s*[-+]\s+/gm, " ")
    .replace(/^\s*\d+[.)]\s+/gm, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function truncate(text, length = 188) {
  if (text.length <= length) return text;
  const candidate = text.slice(0, length - 1).replace(/[，、；：,.!?\s]+$/u, "");
  return `${candidate}…`;
}

function headingText(markdown) {
  return markdown
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/[`*_~]/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

function extractTitle(markdown, filename) {
  const match = markdown.match(/^#\s+(.+)$/m);
  return plainText(match?.[1] ?? filename.replace(/\.md$/i, ""));
}

function extractSummary(markdown) {
  const lines = markdown.split(/\r?\n/);
  const preferredHeading = /^(#{1,3})\s+.*(?:结论先行|先说结论|摘要|Outcome|The recurring shape)/i;
  const preferredIndex = lines.findIndex((line) => preferredHeading.test(line));
  const start = preferredIndex >= 0 ? preferredIndex + 1 : 1;
  let paragraph = [];
  let inFence = false;

  for (let index = start; index < lines.length; index += 1) {
    const trimmed = lines[index].trim();
    if (trimmed.startsWith("```")) {
      inFence = !inFence;
      continue;
    }
    if (inFence || trimmed.startsWith("<!--")) continue;
    if (!trimmed) {
      if (paragraph.length) break;
      continue;
    }
    if (/^#{1,6}\s/.test(trimmed)) {
      if (paragraph.length) break;
      continue;
    }
    if (/^(?:\||[-*+]\s|\d+[.)]\s|>)/.test(trimmed)) {
      if (paragraph.length) break;
      continue;
    }
    paragraph.push(trimmed);
  }

  if (!paragraph.length && preferredIndex >= 0) {
    return extractSummary(lines.slice(preferredIndex + 1).join("\n"));
  }

  return truncate(plainText(paragraph.join(" ")) || "仓库维护的研究与设计记录。");
}

function headingId(title) {
  const id = headingText(title)
    .normalize("NFKC")
    .toLocaleLowerCase("zh-CN")
    .replace(/[^\p{Letter}\p{Number}]+/gu, "-")
    .replace(/^-+|-+$/g, "");
  return id || "section";
}

function extractSections(markdown) {
  const counts = new Map();
  const sections = [];
  for (const line of markdown.split(/\r?\n/)) {
    const match = line.match(/^##\s+(.+)$/);
    if (!match) continue;
    const title = headingText(match[1]);
    const base = headingId(title);
    const occurrence = counts.get(base) ?? 0;
    counts.set(base, occurrence + 1);
    sections.push({
      id: occurrence ? `${base}-${occurrence + 1}` : base,
      title,
    });
  }
  return sections;
}

function slugFromFilename(filename) {
  return filename
    .replace(/\.md$/i, "")
    .toLocaleLowerCase("en-US")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function categoryFor(filename) {
  if (/HARDENING/i.test(filename)) return "工程复盘";
  if (/TRAJECTOR/i.test(filename)) return "运行机制";
  if (/(ROADMAP|PLAN)/i.test(filename)) return "路线与计划";
  if (/DESIGN/i.test(filename)) return "架构设计";
  return "源码调研";
}

const tagRules = [
  ["Agent Runtime", /\bagent\b|harness|runtime|worker|智能体/i],
  ["Workflow", /workflow|工作流|编排/i],
  ["Memory", /memory|记忆|recall/i],
  ["Context", /context|token|上下文|压缩/i],
  ["Security", /security|trust|permission|sandbox|安全|权限|隔离/i],
  ["Durability", /durable|persistence|recovery|持久|恢复|checkpoint/i],
  ["Tools", /\btool|mcp|工具/i],
  ["Architecture", /architecture|架构|模块/i],
];

function tagsFor(markdown) {
  const searchSample = markdown.slice(0, 80_000);
  return tagRules
    .filter(([, pattern]) => pattern.test(searchSample))
    .map(([tag]) => tag)
    .slice(0, 5);
}

function readingMinutes(markdown) {
  const text = plainText(markdown);
  const han = text.match(/[\p{Script=Han}]/gu)?.length ?? 0;
  const latinWords = text.match(/[A-Za-z0-9_./-]+/g)?.length ?? 0;
  return Math.max(1, Math.ceil((han + latinWords * 2.2) / 460));
}

function gitValue(format, relativePath) {
  const result = spawnSync("git", ["log", "-1", `--format=${format}`, "--", relativePath], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  return result.status === 0 ? result.stdout.trim() : "";
}

async function documentRecord(filename) {
  const absolutePath = path.join(docsRoot, filename);
  const relativePath = path.posix.join("docs", filename);
  const [markdown, fileStat] = await Promise.all([
    readFile(absolutePath, "utf8"),
    stat(absolutePath),
  ]);
  const title = extractTitle(markdown, filename);
  const updatedAt = gitValue("%cs", relativePath) || fileStat.mtime.toISOString().slice(0, 10);

  return {
    slug: slugFromFilename(filename),
    title,
    summary: extractSummary(markdown),
    category: categoryFor(filename),
    tags: tagsFor(`${title}\n${markdown}`),
    updatedAt,
    sourceCommit: gitValue("%h", relativePath) || "working-tree",
    sourcePath: relativePath,
    readingMinutes: readingMinutes(markdown),
    sections: extractSections(markdown),
    searchText: plainText(markdown).toLocaleLowerCase("zh-CN"),
    markdown,
  };
}

const filenames = (await readdir(docsRoot))
  .filter((filename) => filename.endsWith(".md") && !excludedDocuments.has(filename))
  .sort((left, right) => left.localeCompare(right, "en"));

const documents = await Promise.all(filenames.map(documentRecord));
documents.sort((left, right) =>
  right.updatedAt.localeCompare(left.updatedAt) || left.title.localeCompare(right.title, "zh-CN"),
);

const payload = `${JSON.stringify({ schemaVersion: 1, documents }, null, 2)}\n`;

if (process.argv.includes("--check")) {
  let existing = "";
  try {
    existing = await readFile(outputPath, "utf8");
  } catch {
    // Missing output is reported as drift below.
  }
  if (existing !== payload) {
    console.error("Research index is out of date. Run `npm run content`. ");
    process.exitCode = 1;
  } else {
    console.log(`Research index is current (${documents.length} documents).`);
  }
} else {
  await mkdir(path.dirname(outputPath), { recursive: true });
  await writeFile(outputPath, payload);
  console.log(`Indexed ${documents.length} documents from docs/*.md.`);
}
