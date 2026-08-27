import type { NextConfig } from "next";

const apiUrl = process.env.INTERNAL_API_URL || process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

const nextConfig: NextConfig = {
  // Emit .next/standalone so the runtime image can ship a traced server plus
  // only the node_modules it actually loads, instead of the full install.
  output: "standalone",
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
