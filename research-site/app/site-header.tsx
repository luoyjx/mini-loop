/* eslint-disable @next/next/no-html-link-for-pages -- vinext Link crashes after production deployment. */

type SiteHeaderProps = {
  detail?: string;
};

export function SiteHeader({ detail = "mini-loop / docs" }: SiteHeaderProps) {
  return (
    <header className="topbar">
      <a className="brand" href="/" aria-label="mini-loop Research Atlas 首页">
        <span className="brand-mark" aria-hidden="true">ml</span>
        <span>Research Atlas</span>
      </a>
      <span className="repository-label">{detail}</span>
    </header>
  );
}
