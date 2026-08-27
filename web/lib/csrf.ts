// Double-submit CSRF pair shared by the browser client and the /api/v1/auth
// route handlers. The browser mints a random value, keeps it in a
// JS-readable cookie, and repeats it in a request header; the route handler
// only accepts the request when the two match. A cross-site page can neither
// read the cookie nor attach the custom header (a form cannot set headers,
// and a cross-origin fetch with one is stopped by CORS), so it cannot forge
// the pair. This is defense in depth on top of the SameSite=strict httpOnly
// refresh cookie, and it also blocks login CSRF, which SameSite alone does
// not cover.

export const CSRF_COOKIE_NAME = "tabmail_csrf";
export const CSRF_HEADER_NAME = "X-CSRF-Token";

const CSRF_COOKIE_MAX_AGE_SECONDS = 365 * 24 * 60 * 60;

function readCsrfCookie(): string | null {
  const prefix = `${CSRF_COOKIE_NAME}=`;
  for (const part of document.cookie.split("; ")) {
    if (part.startsWith(prefix)) {
      const value = part.slice(prefix.length);
      return value || null;
    }
  }
  return null;
}

function mintCsrfToken(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

// ensureCsrfToken returns the browser's CSRF token, minting and storing one
// on first use. The token is not a server secret: any value works as long as
// the cookie and the header agree. Returns null outside the browser.
export function ensureCsrfToken(): string | null {
  if (typeof document === "undefined") return null;
  const existing = readCsrfCookie();
  if (existing) return existing;
  const token = mintCsrfToken();
  // Path=/ so page scripts can read it back; the value guards nothing by
  // itself. SameSite=Strict keeps even this cookie out of cross-site
  // requests.
  const secure = window.location.protocol === "https:" ? "; Secure" : "";
  document.cookie = `${CSRF_COOKIE_NAME}=${token}; Path=/; SameSite=Strict; Max-Age=${CSRF_COOKIE_MAX_AGE_SECONDS}${secure}`;
  return token;
}
