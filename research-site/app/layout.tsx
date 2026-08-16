import type { Metadata } from "next";
import { headers } from "next/headers";
import "./globals.css";

const siteTitle = "mini-loop Research Atlas";
const siteDescription = "浏览 mini-loop 仓库中的源码调研、架构判断、证据边界与落地计划。";

function safeOrigin(forwardedHost: string | null, host: string | null, protocol: string | null) {
  const candidate = (forwardedHost ?? host ?? "localhost:3000").split(",")[0].trim();
  const safeHost = /^[a-z0-9.-]+(?::\d+)?$/i.test(candidate) ? candidate : "localhost:3000";
  const safeProtocol = protocol === "http" || protocol === "https"
    ? protocol
    : safeHost.startsWith("localhost")
      ? "http"
      : "https";
  return `${safeProtocol}://${safeHost}`;
}

export async function generateMetadata(): Promise<Metadata> {
  const requestHeaders = await headers();
  const origin = safeOrigin(
    requestHeaders.get("x-forwarded-host"),
    requestHeaders.get("host"),
    requestHeaders.get("x-forwarded-proto"),
  );
  const socialImage = `${origin}/og.png`;

  return {
    metadataBase: new URL(origin),
    title: siteTitle,
    description: siteDescription,
    alternates: { canonical: origin },
    openGraph: {
      title: siteTitle,
      description: siteDescription,
      type: "website",
      siteName: siteTitle,
      url: origin,
      images: [
        {
          url: socialImage,
          width: 1730,
          height: 909,
          alt: "mini-loop Research Atlas",
        },
      ],
    },
    twitter: {
      card: "summary_large_image",
      title: siteTitle,
      description: siteDescription,
      images: [socialImage],
    },
  };
}

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN">
      <body>
        <a className="skip-link" href="#main-content">跳到正文</a>
        {children}
      </body>
    </html>
  );
}
