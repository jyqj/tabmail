import type { NextConfig } from "next";

const apiUrl = process.env.INTERNAL_API_URL || process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      { source: "/api/v1/:path*", destination: `${apiUrl}/api/v1/:path*` },
      { source: "/health", destination: `${apiUrl}/health` },
      { source: "/openapi.yaml", destination: `${apiUrl}/openapi.yaml` },
      { source: "/backend-docs", destination: `${apiUrl}/docs` },
      { source: "/backend-redoc", destination: `${apiUrl}/redoc` },
      { source: "/metrics", destination: `${apiUrl}/metrics` },
    ];
  },
};

export default nextConfig;
