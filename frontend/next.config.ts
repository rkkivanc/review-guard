import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // web-llm uses WebGPU / WASM only in the browser
  webpack: (config) => {
    config.resolve.fallback = {
      ...config.resolve.fallback,
      fs: false,
      path: false,
      crypto: false,
    };
    return config;
  },
};

export default nextConfig;
