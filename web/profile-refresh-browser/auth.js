import { useSessionScope } from '@/lib/session';
export function useAuth() { useSessionScope(); return window.review.auth; }
