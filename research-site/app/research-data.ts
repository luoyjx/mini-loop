import generated from "./data/research.generated.json";

export type ResearchSection = {
  id: string;
  title: string;
};

export type ResearchDocument = {
  slug: string;
  title: string;
  summary: string;
  category: string;
  tags: string[];
  updatedAt: string;
  sourceCommit: string;
  sourcePath: string;
  readingMinutes: number;
  sections: ResearchSection[];
  searchText: string;
  markdown: string;
};

export type ResearchIndexItem = Omit<ResearchDocument, "markdown">;

export const researchDocuments = generated.documents as ResearchDocument[];

export const researchIndex: ResearchIndexItem[] = researchDocuments.map(
  ({ markdown: _markdown, ...document }) => document,
);

export function getResearchDocument(slug: string) {
  return researchDocuments.find((document) => document.slug === slug);
}

export function relatedResearch(document: ResearchDocument, limit = 3) {
  const tags = new Set(document.tags);
  return researchDocuments
    .filter((candidate) => candidate.slug !== document.slug)
    .map((candidate) => ({
      candidate,
      score: candidate.tags.filter((tag) => tags.has(tag)).length,
    }))
    .sort(
      (left, right) =>
        right.score - left.score ||
        right.candidate.updatedAt.localeCompare(left.candidate.updatedAt),
    )
    .slice(0, limit)
    .map(({ candidate }) => candidate);
}

export function formatResearchDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "short",
    day: "numeric",
    timeZone: "Asia/Shanghai",
  }).format(new Date(`${value}T00:00:00+08:00`));
}
