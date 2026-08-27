import type { Messages, Translate } from "./i18n-types";
import { zh } from "./messages/zh";

// Shared by the client provider and the server helper so a string reads the
// same wherever it is rendered. The default catalog backs every lookup, which
// keeps a key that is only translated in zh from rendering as a raw key.
export function createTranslate(messages: Messages): Translate {
  return (key, params) => {
    let msg = messages[key] ?? zh[key] ?? key;
    if (!params) return msg;
    msg = msg.replace(/\{(\w+)\|([^|]*)\|([^}]*)}/g, (match, name, singular, plural) => {
      const value = params[name];
      return value !== undefined ? (Number(value) === 1 ? singular : plural) : match;
    });
    return Object.entries(params).reduce(
      (out, [name, value]) => out.replaceAll(`{${name}}`, String(value)),
      msg,
    );
  };
}
