import type { NextConfig } from "next";

const apiTarget = process.env.API_PROXY_TARGET || "http://127.0.0.1:8787";
const isExport = process.env.NEXT_OUTPUT === "export" || process.env.NODE_ENV === "production";

const nextConfig: NextConfig = {
  // Static export → copied into web/ for Go embed (production)
  ...(isExport ? { output: "export" } : {}),
  trailingSlash: true,
  images: { unoptimized: true },
  turbopack: {
    root: process.cwd(),
  },
};

// Rewrites are only used by `next dev`; production remains a static export.
if (!isExport) {
  nextConfig.rewrites = async () => [
    { source: "/api/:path*", destination: `${apiTarget}/api/:path*` },
    { source: "/mcp", destination: `${apiTarget}/mcp` },
  ];
}

export default nextConfig;
