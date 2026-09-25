"use client";
import { useEffect, useRef, useState } from "react";
import { ActionButton, inputClass, useText } from "@/components/company/common";
const tags = new Set(["P", "DIV", "BR", "STRONG", "B", "EM", "I", "U", "UL", "OL", "LI", "BLOCKQUOTE", "A"]);
// This editor emits a small formatting vocabulary. The server still sanitizes
// on send and all received HTML is rendered in a sandbox, never in this editor.
export function cleanEditorHTML(raw: string): string {
    const document = new DOMParser().parseFromString(raw, "text/html");
    for (const node of Array.from(document.body.querySelectorAll("*"))) {
        if (["SCRIPT", "STYLE", "IFRAME", "OBJECT", "SVG", "MATH", "FORM", "IMG", "VIDEO", "AUDIO"].includes(node.tagName)) {
            node.remove();
            continue;
        }
        if (!tags.has(node.tagName)) {
            node.replaceWith(...Array.from(node.childNodes));
            continue;
        }
        const href = node.getAttribute("href");
        for (const attr of Array.from(node.attributes))
            node.removeAttribute(attr.name);
        if (node.tagName === "A" && href && /^(https?:\/\/|mailto:)/i.test(href.trim())) {
            node.setAttribute("href", href.trim());
            node.setAttribute("rel", "noreferrer noopener");
        }
    }
    return document.body.innerHTML;
}
function escaped(text: string) {
    return text.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll("\n", "<br>");
}
export function RichMessage({ id, text, html, disabled, onChange }: {
    id: string;
    text: string;
    html?: string;
    disabled?: boolean;
    onChange: (value: {
        text_body: string;
        html_body?: string;
    }) => void;
}) {
    const t = useText();
    // Plain text remains first-class and backwards compatible, including RFC quotes.
    const [rich, setRich] = useState(false);
    const editor = useRef<HTMLDivElement>(null);
    useEffect(() => {
        const el = editor.current;
        if (rich && el && document.activeElement !== el)
            el.innerHTML = cleanEditorHTML(html || escaped(text));
    }, [rich, html, text]);
    const emit = () => {
        const el = editor.current;
        if (el)
            onChange({ text_body: el.innerText ?? el.textContent ?? "", html_body: cleanEditorHTML(el.innerHTML) });
    };
    const format = (tag: "strong" | "em" | "u") => {
        const selection = window.getSelection();
        if (!selection?.rangeCount || !editor.current?.contains(selection.anchorNode))
            return;
        const range = selection.getRangeAt(0);
        if (!editor.current.contains(range.commonAncestorContainer) || range.collapsed)
            return;
        const span = document.createElement(tag);
        span.appendChild(range.extractContents());
        range.insertNode(span);
        range.selectNodeContents(span);
        selection.removeAllRanges();
        selection.addRange(range);
        emit();
    };
    return <div className="space-y-2">
    <div className="flex flex-wrap gap-2">
      <ActionButton disabled={disabled} aria-pressed={rich} onClick={() => {
            if (rich && !window.confirm(t("切换纯文本会移除排版，继续？", "Switching to plain text removes formatting. Continue?")))
                return;
            if (rich)
                onChange({ text_body: text, html_body: undefined });
            setRich(!rich);
        }}>{rich ? t("切换纯文本", "Use plain text") : t("格式化编辑", "Formatting editor")}</ActionButton>
      {rich && (["strong", "em", "u"] as const).map((tag, i) => <ActionButton key={tag} disabled={disabled} onMouseDown={e => e.preventDefault()} onClick={() => format(tag)}>{[t("粗体", "Bold"), t("斜体", "Italic"), t("下划线", "Underline")][i]}</ActionButton>)}
    </div>
    {rich ? <div id={id} ref={editor} role="textbox" aria-multiline="true" aria-label={t("格式化正文", "Formatted message")} contentEditable={!disabled} suppressContentEditableWarning className={`${inputClass} min-h-64 whitespace-pre-wrap`} onDrop={event => event.preventDefault()} onDragOver={event => event.preventDefault()} onInput={emit} onPaste={event => {
                // Paste text only; clipboard HTML cannot introduce images, trackers,
                // executable attributes or remote content into the editor DOM.
                event.preventDefault();
                const value = event.clipboardData.getData("text/plain");
                const sel = window.getSelection();
                if (!sel?.rangeCount || !editor.current?.contains(sel.anchorNode))
                    return;
                const range = sel.getRangeAt(0);
                range.deleteContents();
                const node = document.createTextNode(value);
                range.insertNode(node);
                range.setStartAfter(node);
                range.collapse(true);
                sel.removeAllRanges();
                sel.addRange(range);
                emit();
            }}/> : <textarea id={id} className={inputClass} rows={10} disabled={disabled} value={text} onChange={e => onChange({ text_body: e.target.value, html_body: undefined })}/>}
    {rich && <p className="text-xs text-muted-foreground">{t("选中文字后使用格式按钮。粘贴仅保留文字，邮件同时生成纯文本正文。", "Select text to apply formatting. Paste is text-only; a plain-text body is saved too.")}</p>}
  </div>;
}
