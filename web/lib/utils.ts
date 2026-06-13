import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// window.confirm wrapper that degrades to "confirmed" in environments
// where confirm is unavailable or throws (SSR, some embedded webviews).
export function safeConfirm(message: string) {
  if (typeof window === "undefined" || typeof window.confirm !== "function") return true;
  try {
    return window.confirm(message) !== false;
  } catch {
    return true;
  }
}
