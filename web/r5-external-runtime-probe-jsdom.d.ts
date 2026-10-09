// Minimal API used only by the external runtime self-probe. Keep the locked
// runtime dependency unchanged; this describes the real DOM surface we inspect.
declare module "jsdom" {
  export class JSDOM {
    constructor(html: string);
    readonly window: { readonly document: Document };
  }
}
