"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { ResearchIndexItem } from "./research-data";
import { SiteHeader } from "./site-header";

type ResearchIndexProps = {
  documents: ResearchIndexItem[];
};

function formatDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "short",
    day: "numeric",
    timeZone: "Asia/Shanghai",
  }).format(new Date(`${value}T00:00:00+08:00`));
}

function normalized(value: string) {
  return value.toLocaleLowerCase("zh-CN").trim();
}

export function ResearchIndex({ documents }: ResearchIndexProps) {
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState("全部");
  const searchRef = useRef<HTMLInputElement>(null);
  const categories = useMemo(
    () => ["全部", ...Array.from(new Set(documents.map((document) => document.category)))],
    [documents],
  );
  const totalMinutes = documents.reduce(
    (total, document) => total + document.readingMinutes,
    0,
  );
  const latest = documents[0];

  useEffect(() => {
    function focusSearch(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLocaleLowerCase() === "k") {
        event.preventDefault();
        searchRef.current?.focus();
      }
    }

    window.addEventListener("keydown", focusSearch);
    return () => window.removeEventListener("keydown", focusSearch);
  }, []);

  const visibleDocuments = useMemo(() => {
    const needle = normalized(query);
    return documents.filter((document) => {
      if (category !== "全部" && document.category !== category) return false;
      if (!needle) return true;
      const haystack = normalized(
        [
          document.title,
          document.summary,
          document.category,
          document.tags.join(" "),
          document.searchText,
        ].join(" "),
      );
      return haystack.includes(needle);
    });
  }, [category, documents, query]);

  return (
    <div className="site-shell index-page">
      <SiteHeader />

      <main id="main-content">
        <section className="hero index-hero" aria-labelledby="page-title">
          <div className="hero-copy">
            <p className="overline">Evidence-led repository research</p>
            <h1 id="page-title">把每次调研，变成可以继续探索的知识地图。</h1>
            <p className="hero-lede">
              汇总 mini-loop 的源码调研、架构判断与落地边界。内容直接来自仓库原文，新增文档会在构建时进入索引。
            </p>
            {latest ? (
              <a className="latest-link" href={`/research/${latest.slug}`}>
                <span>最近更新</span>
                <strong>{latest.title}</strong>
                <span aria-hidden="true">→</span>
              </a>
            ) : null}
          </div>

          <dl className="hero-metrics" aria-label="研究库概况">
            <div>
              <dt>文档</dt>
              <dd>{documents.length}</dd>
            </div>
            <div>
              <dt>主题</dt>
              <dd>{Math.max(0, categories.length - 1)}</dd>
            </div>
            <div>
              <dt>阅读</dt>
              <dd>{totalMinutes}<span> min</span></dd>
            </div>
          </dl>
        </section>

        <section className="catalog" aria-labelledby="catalog-title">
          <div className="catalog-heading">
            <div>
              <p className="overline">Research index</p>
              <h2 id="catalog-title">从问题出发，而不是从文件名出发</h2>
            </div>
            <label className="search-field" htmlFor="research-search">
              <span>搜索标题、结论、章节与关键词</span>
              <span className="search-control">
                <input
                  id="research-search"
                  ref={searchRef}
                  type="search"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="例如：memory、workflow、token…"
                  autoComplete="off"
                />
                <kbd>⌘ K</kbd>
              </span>
            </label>
          </div>

          <div className="catalog-toolbar">
            <div className="category-list" aria-label="按文档类型筛选">
              {categories.map((item) => (
                <button
                  className={item === category ? "category-chip is-active" : "category-chip"}
                  key={item}
                  type="button"
                  aria-pressed={item === category}
                  onClick={() => setCategory(item)}
                >
                  {item}
                </button>
              ))}
            </div>
            <p className="result-count" aria-live="polite">
              显示 {visibleDocuments.length} / {documents.length}
            </p>
          </div>

          {visibleDocuments.length ? (
            <div className="research-grid">
              {visibleDocuments.map((document, index) => (
                <a
                  className="research-card"
                  href={`/research/${document.slug}`}
                  key={document.slug}
                  aria-label={`阅读：${document.title}`}
                >
                  <div className="card-topline">
                    <span className="card-index">{String(index + 1).padStart(2, "0")}</span>
                    <span>{formatDate(document.updatedAt)}</span>
                  </div>
                  <p className="card-eyebrow">{document.category}</p>
                  <h3>{document.title}</h3>
                  <p className="card-summary">{document.summary}</p>
                  <div className="tag-list" aria-label="主题标签">
                    {document.tags.slice(0, 3).map((tag) => (
                      <span key={tag}>{tag}</span>
                    ))}
                  </div>
                  <div className="card-footer">
                    <span>{document.readingMinutes} 分钟</span>
                    <span>阅读全文 <span aria-hidden="true">→</span></span>
                  </div>
                </a>
              ))}
            </div>
          ) : (
            <div className="empty-state" role="status">
              <p className="overline">No match</p>
              <h3>暂时没有匹配的调研</h3>
              <p>试试更短的关键词，或清除分类筛选后重新搜索。</p>
              <button
                type="button"
                onClick={() => {
                  setQuery("");
                  setCategory("全部");
                  searchRef.current?.focus();
                }}
              >
                清除筛选
              </button>
            </div>
          )}
        </section>
      </main>

      <footer className="site-footer">
        <p>Source of truth: <code>docs/*.md</code></p>
        <p>Built from the current mini-loop checkout.</p>
      </footer>
    </div>
  );
}
