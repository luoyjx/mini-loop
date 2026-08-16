import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { MarkdownDocument } from "../../markdown-document";
import {
  formatResearchDate,
  getResearchDocument,
  relatedResearch,
  researchDocuments,
} from "../../research-data";
import { SiteHeader } from "../../site-header";

type ResearchPageProps = {
  params: Promise<{ slug: string }>;
};

export function generateStaticParams() {
  return researchDocuments.map((document) => ({ slug: document.slug }));
}

export async function generateMetadata({ params }: ResearchPageProps): Promise<Metadata> {
  const { slug } = await params;
  const document = getResearchDocument(slug);
  if (!document) return {};

  const title = `${document.title} | mini-loop Research Atlas`;
  const description = document.summary;
  return {
    title,
    description,
    openGraph: {
      title,
      description,
      type: "article",
      images: [],
    },
    twitter: {
      card: "summary",
      title,
      description,
      images: [],
    },
  };
}

export default async function ResearchPage({ params }: ResearchPageProps) {
  const { slug } = await params;
  const document = getResearchDocument(slug);
  if (!document) notFound();
  const related = relatedResearch(document);

  return (
    <div className="site-shell detail-page">
      <SiteHeader detail={document.category} />

      <main id="main-content">
        <nav className="breadcrumb" aria-label="面包屑导航">
          <Link href="/">研究索引</Link>
          <span aria-hidden="true">/</span>
          <span>{document.category}</span>
        </nav>

        <header className="article-hero">
          <p className="overline">{document.category}</p>
          <h1>{document.title}</h1>
          <p className="article-summary">{document.summary}</p>
          <dl className="article-meta">
            <div>
              <dt>更新</dt>
              <dd>{formatResearchDate(document.updatedAt)}</dd>
            </div>
            <div>
              <dt>阅读</dt>
              <dd>约 {document.readingMinutes} 分钟</dd>
            </div>
            <div>
              <dt>来源</dt>
              <dd><code>{document.sourcePath}</code></dd>
            </div>
            <div>
              <dt>版本</dt>
              <dd><code>{document.sourceCommit}</code></dd>
            </div>
          </dl>
          <div className="tag-list article-tags" aria-label="主题标签">
            {document.tags.map((tag) => <span key={tag}>{tag}</span>)}
          </div>
        </header>

        <div className="article-layout">
          <aside className="article-rail">
            <details className="toc" open>
              <summary>本文目录</summary>
              <nav aria-label="本文目录">
                {document.sections.map((section) => (
                  <a href={`#${section.id}`} key={section.id}>{section.title}</a>
                ))}
              </nav>
            </details>
            <Link className="back-link" href="/">
              <span aria-hidden="true">←</span> 返回研究索引
            </Link>
          </aside>

          <article className="markdown-body">
            <MarkdownDocument
              markdown={document.markdown}
              sourceCommit={document.sourceCommit}
              sourcePath={document.sourcePath}
            />
          </article>
        </div>

        <section className="related-section" aria-labelledby="related-title">
          <div>
            <p className="overline">Continue exploring</p>
            <h2 id="related-title">沿相邻主题继续阅读</h2>
          </div>
          <div className="related-list">
            {related.map((item) => (
              <Link href={`/research/${item.slug}`} key={item.slug}>
                <span>{item.category}</span>
                <strong>{item.title}</strong>
                <span aria-hidden="true">→</span>
              </Link>
            ))}
          </div>
        </section>
      </main>

      <footer className="site-footer">
        <p>原文是唯一内容权威；网页是可重新生成的阅读视图。</p>
        <p><code>{document.sourcePath}</code></p>
      </footer>
    </div>
  );
}
