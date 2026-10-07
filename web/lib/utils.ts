import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// Destructive actions require explicit user confirmation. An unavailable or
// blocked browser dialog never grants permission to proceed.
export function safeConfirm(message: string): boolean {
  try {
    if (typeof window === "undefined") return false;
    const confirm = window.confirm;
    return typeof confirm === "function" && confirm.call(window, message) === true;
  } catch {
    return false;
  }
}
