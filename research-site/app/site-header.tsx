import Link from "next/link";

type SiteHeaderProps = {
  detail?: string;
};

export function SiteHeader({ detail = "mini-loop / docs" }: SiteHeaderProps) {
  return (
    <header className="topbar">
      <Link className="brand" href="/" aria-label="mini-loop Research Atlas 首页">
        <span className="brand-mark" aria-hidden="true">ml</span>
        <span>Research Atlas</span>
      </Link>
      <span className="repository-label">{detail}</span>
    </header>
  );
}
