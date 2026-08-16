import { createElement, Fragment, type ReactNode } from "react";
import { researchDocuments } from "./research-data";

type MarkdownDocumentProps = {
  markdown: string;
  sourceCommit: string;
  sourcePath: string;
};

type LinkContext = Omit<MarkdownDocumentProps, "markdown">;

const researchRouteByFilename = new Map(
  researchDocuments.map((document) => [
    document.sourcePath.split("/").at(-1)?.toLocaleLowerCase("en-US"),
    document.slug,
  ]),
);

function plainHeading(value: string) {
  return value
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/[`*_~]/g, "")
    .trim();
}

function headingBase(value: string) {
  return plainHeading(value)
    .normalize("NFKC")
    .toLocaleLowerCase("zh-CN")
    .replace(/[^\p{Letter}\p{Number}]+/gu, "-")
    .replace(/^-+|-+$/g, "") || "section";
}

function repositoryPath(sourcePath: string, target: string) {
  const parts = sourcePath.split("/");
  parts.pop();
  for (const segment of target.split("/")) {
    if (!segment || segment === ".") continue;
    if (segment === "..") parts.pop();
    else parts.push(segment);
  }
  return parts.join("/");
}

function resolvedHref(href: string, context: LinkContext) {
  if (/^https?:\/\//i.test(href) || href.startsWith("mailto:")) return href;
  if (/^[a-z][a-z0-9+.-]*:/i.test(href)) return "#";
  if (href.startsWith("#") || href.startsWith("/")) return href;

  const markdownLink = href.match(/(?:^|\/)([^/#]+\.md)(?:#(.+))?$/i);
  if (markdownLink) {
    const knownSlug = researchRouteByFilename.get(markdownLink[1].toLocaleLowerCase("en-US"));
    if (knownSlug) {
      const anchor = markdownLink[2] ? `#${headingBase(decodeURIComponent(markdownLink[2]))}` : "";
      return `/research/${knownSlug}${anchor}`;
    }
  }

  const [target, fragment] = href.split("#", 2);
  const resolvedPath = repositoryPath(context.sourcePath, target);
  const revision = context.sourceCommit === "working-tree" ? "main" : context.sourceCommit;
  const view = target.endsWith("/") ? "tree" : "blob";
  return `https://github.com/luoyjx/mini-loop/${view}/${revision}/${resolvedPath}${fragment ? `#${fragment}` : ""}`;
}

function inline(value: string, keyPrefix: string, context: LinkContext): ReactNode[] {
  const pattern = /(`[^`]+`|\*\*[^*]+\*\*|__[^_]+__|!\[[^\]]*\]\([^)]+\)|\[[^\]]+\]\([^)]+\)|<https?:\/\/[^>]+>|<br\s*\/?\s*>|\*[^*\n]+\*|_[^_\n]+_)/gi;
  const nodes: ReactNode[] = [];
  let cursor = 0;
  let tokenIndex = 0;

  for (const match of value.matchAll(pattern)) {
    const start = match.index ?? 0;
    if (start > cursor) nodes.push(value.slice(cursor, start));
    const token = match[0];
    const key = `${keyPrefix}-${tokenIndex}`;

    if (token.startsWith("`")) {
      nodes.push(<code key={key}>{token.slice(1, -1)}</code>);
    } else if (token.startsWith("**") || token.startsWith("__")) {
      nodes.push(<strong key={key}>{inline(token.slice(2, -2), `${key}-strong`, context)}</strong>);
    } else if (token.startsWith("![")) {
      const image = token.match(/^!\[([^\]]*)\]\(([^)]+)\)$/);
      nodes.push(
        <span className="image-reference" key={key}>
          图：{image?.[1] || image?.[2] || "原文图片"}
        </span>,
      );
    } else if (token.startsWith("[")) {
      const link = token.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
      const href = resolvedHref(link?.[2] ?? "#", context);
      const external = /^https?:\/\//i.test(href);
      nodes.push(
        <a
          href={href}
          key={key}
          rel={external ? "noreferrer" : undefined}
          target={external ? "_blank" : undefined}
        >
          {inline(link?.[1] ?? token, `${key}-link`, context)}
        </a>,
      );
    } else if (token.toLocaleLowerCase().startsWith("<br")) {
      nodes.push(<br key={key} />);
    } else if (token.startsWith("<http")) {
      const href = token.slice(1, -1);
      nodes.push(<a href={href} key={key} rel="noreferrer" target="_blank">{href}</a>);
    } else {
      nodes.push(<em key={key}>{inline(token.slice(1, -1), `${key}-em`, context)}</em>);
    }

    cursor = start + token.length;
    tokenIndex += 1;
  }

  if (cursor < value.length) nodes.push(value.slice(cursor));
  return nodes;
}

function tableCells(line: string) {
  return line
    .trim()
    .replace(/^\||\|$/g, "")
    .split("|")
    .map((cell) => cell.trim());
}

function isTableDivider(line: string) {
  return /^\s*\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(line);
}

export function MarkdownDocument({ markdown, sourceCommit, sourcePath }: MarkdownDocumentProps) {
  const linkContext = { sourceCommit, sourcePath };
  const lines = markdown.split(/\r?\n/);
  const blocks: ReactNode[] = [];
  const h2Counts = new Map<string, number>();
  const otherHeadingCounts = new Map<string, number>();
  let index = 0;
  let skippedTitle = false;

  function uniqueHeadingId(title: string, level: number) {
    const base = headingBase(title);
    const counts = level === 2 ? h2Counts : otherHeadingCounts;
    const occurrence = counts.get(base) ?? 0;
    counts.set(base, occurrence + 1);
    return occurrence ? `${base}-${occurrence + 1}` : base;
  }

  while (index < lines.length) {
    const raw = lines[index];
    const trimmed = raw.trim();

    if (!trimmed) {
      index += 1;
      continue;
    }

    if (trimmed.startsWith("<!--")) {
      while (index < lines.length && !lines[index].includes("-->")) index += 1;
      index += 1;
      continue;
    }

    const fence = trimmed.match(/^```([^\s]*)/);
    if (fence) {
      const language = fence[1] || "text";
      const code: string[] = [];
      index += 1;
      while (index < lines.length && !lines[index].trim().startsWith("```")) {
        code.push(lines[index]);
        index += 1;
      }
      index += 1;
      blocks.push(
        <figure className="code-block" key={`code-${index}`}>
          <figcaption>{language === "mermaid" ? "Mermaid diagram source" : language}</figcaption>
          <pre><code>{code.join("\n")}</code></pre>
        </figure>,
      );
      continue;
    }

    const heading = trimmed.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      const level = heading[1].length;
      const title = heading[2].replace(/\s+#+$/, "");
      if (level === 1 && !skippedTitle) {
        skippedTitle = true;
        index += 1;
        continue;
      }
      const id = uniqueHeadingId(title, level);
      blocks.push(
        createElement(
          `h${level}`,
          { id, key: `heading-${index}`, tabIndex: -1 },
          inline(title, `heading-${index}`, linkContext),
        ),
      );
      index += 1;
      continue;
    }

    if (/^(?:-{3,}|\*{3,}|_{3,})$/.test(trimmed)) {
      blocks.push(<hr key={`hr-${index}`} />);
      index += 1;
      continue;
    }

    if (trimmed.startsWith("|") && index + 1 < lines.length && isTableDivider(lines[index + 1])) {
      const headers = tableCells(lines[index]);
      const rows: string[][] = [];
      index += 2;
      while (index < lines.length && lines[index].trim().startsWith("|")) {
        rows.push(tableCells(lines[index]));
        index += 1;
      }
      blocks.push(
        <div className="table-scroll" key={`table-${index}`} tabIndex={0} role="region" aria-label="可横向滚动的数据表">
          <table>
            <thead>
              <tr>{headers.map((cell, cellIndex) => <th key={cellIndex}>{inline(cell, `th-${index}-${cellIndex}`, linkContext)}</th>)}</tr>
            </thead>
            <tbody>
              {rows.map((row, rowIndex) => (
                <tr key={rowIndex}>
                  {row.map((cell, cellIndex) => <td key={cellIndex}>{inline(cell, `td-${index}-${rowIndex}-${cellIndex}`, linkContext)}</td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>,
      );
      continue;
    }

    if (trimmed.startsWith(">")) {
      const quote: string[] = [];
      while (index < lines.length && lines[index].trim().startsWith(">")) {
        quote.push(lines[index].trim().replace(/^>\s?/, ""));
        index += 1;
      }
      blocks.push(<blockquote key={`quote-${index}`}><p>{inline(quote.join(" "), `quote-${index}`, linkContext)}</p></blockquote>);
      continue;
    }

    const listMatch = trimmed.match(/^([-*+]|\d+[.)])\s+(.+)$/);
    if (listMatch) {
      const ordered = /^\d/.test(listMatch[1]);
      const items: string[] = [];
      while (index < lines.length) {
        const current = lines[index].trim().match(/^([-*+]|\d+[.)])\s+(.+)$/);
        if (!current || /^\d/.test(current[1]) !== ordered) break;
        let item = current[2];
        index += 1;
        while (
          index < lines.length &&
          lines[index].trim() &&
          !/^([-*+]|\d+[.)])\s+/.test(lines[index].trim()) &&
          !/^(#{1,6})\s+/.test(lines[index].trim())
        ) {
          item += ` ${lines[index].trim()}`;
          index += 1;
        }
        items.push(item);
      }
      const List = ordered ? "ol" : "ul";
      blocks.push(
        <List key={`list-${index}`}>
          {items.map((item, itemIndex) => <li key={itemIndex}>{inline(item, `list-${index}-${itemIndex}`, linkContext)}</li>)}
        </List>,
      );
      continue;
    }

    if (/^ {4}/.test(raw)) {
      const code: string[] = [];
      while (index < lines.length && (/^ {4}/.test(lines[index]) || !lines[index].trim())) {
        code.push(lines[index].replace(/^ {4}/, ""));
        index += 1;
      }
      blocks.push(<pre key={`indented-code-${index}`}><code>{code.join("\n")}</code></pre>);
      continue;
    }

    const paragraph: string[] = [trimmed];
    index += 1;
    while (index < lines.length) {
      const next = lines[index].trim();
      if (
        !next ||
        /^(#{1,6})\s+/.test(next) ||
        /^```/.test(next) ||
        /^(?:[-*+]|\d+[.)])\s+/.test(next) ||
        next.startsWith(">") ||
        next.startsWith("|") ||
        /^(?:-{3,}|\*{3,}|_{3,})$/.test(next)
      ) break;
      paragraph.push(next);
      index += 1;
    }
    blocks.push(<p key={`paragraph-${index}`}>{inline(paragraph.join(" "), `paragraph-${index}`, linkContext)}</p>);
  }

  return <Fragment>{blocks}</Fragment>;
}
