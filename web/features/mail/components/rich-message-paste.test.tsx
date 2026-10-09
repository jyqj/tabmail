import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { RichMessage } from "./rich-message";

// Actual RichMessage/common buttons and jsdom Selection/Range. Only the paste
// event's clipboard is synthetic: this is not a browser/native clipboard gate.
afterEach(() => { window.getSelection()?.removeAllRanges(); cleanup(); });
function mount(text = "abcdef", html?: string, second = false) {
  const onChange = vi.fn(), onOtherChange = vi.fn();
  const view = (disabled = false) => <main data-testid="host">
    <p data-testid="before">before-untouched</p>
    <RichMessage id="body" text={text} html={html} disabled={disabled} onChange={onChange} />
    <p data-testid="after">after-untouched</p>
    {second && <RichMessage id="other-body" text="other-editor" onChange={onOtherChange} />}
  </main>;
  function Host() {
    const [disabled, setDisabled] = useState(false);
    return <><button onClick={() => setDisabled(true)}>Disable editor</button><button onClick={() => setDisabled(false)}>Enable editor</button>{view(disabled)}</>;
  }
  const result = render(<Host />);
  for (const button of screen.queryAllByRole("button", { name: "Formatting editor" })) fireEvent.click(button);
  const editor = screen.getAllByRole("textbox", { name: "Formatted message" })[0];
  expect(editor).toHaveAttribute("contenteditable", "true");
  return { ...result, editor, onChange, onOtherChange, host: screen.getByTestId("host"),
    before: screen.getByTestId("before").firstChild!, after: screen.getByTestId("after").firstChild!,
    other: second ? screen.getAllByRole("textbox", { name: "Formatted message" })[1] : undefined,
    setDisabled: (disabled: boolean) => fireEvent.click(screen.getByRole("button", { name: disabled ? "Disable editor" : "Enable editor" })) };
}
function select(anchor: Node, anchorOffset: number, focus = anchor, focusOffset = anchorOffset) {
  const selection = window.getSelection()!;
  selection.setBaseAndExtent(anchor, anchorOffset, focus, focusOffset);
  // Do not fake anchor/focus independently of the actual ordered Range.
  expect(selection.rangeCount).toBe(1); expect(selection.anchorNode).toBe(anchor); expect(selection.anchorOffset).toBe(anchorOffset);
  expect(selection.focusNode).toBe(focus); expect(selection.focusOffset).toBe(focusOffset);
  return selection.getRangeAt(0);
}
function selectionSnapshot() {
  const s = window.getSelection()!, r = s.rangeCount ? s.getRangeAt(0) : null;
  return { count: s.rangeCount, anchor: s.anchorNode, anchorOffset: s.anchorOffset, focus: s.focusNode, focusOffset: s.focusOffset,
    start: r?.startContainer, startOffset: r?.startOffset, end: r?.endContainer, endOffset: r?.endOffset, common: r?.commonAncestorContainer };
}
function paste(editor: HTMLElement, text = "PASTED", html = '<img src="https://tracker.example.test/pixel" onerror="alert(1)">') {
  const event = new Event("paste", { bubbles: true, cancelable: true });
  const getData = vi.fn((type: string) => type === "text/plain" ? text : type === "text/html" ? html : "");
  Object.defineProperty(event, "clipboardData", { value: { getData } });
  fireEvent(editor, event); expect(event.defaultPrevented).toBe(true); return getData;
}
function reject(context: ReturnType<typeof mount>) {
  const markup = context.host.innerHTML, selection = selectionSnapshot();
  paste(context.editor);
  expect(context.host.innerHTML).toBe(markup);
  expect(context.onChange).not.toHaveBeenCalled(); expect(context.onOtherChange).not.toHaveBeenCalled();
  const current = selectionSnapshot();
  for (const key of Object.keys(selection) as (keyof typeof selection)[]) expect(current[key]).toBe(selection[key]);
}
function accepted(context: ReturnType<typeof mount>, pasted: string, expected: string) {
  const getData = paste(context.editor, pasted);
  expect(getData.mock.calls).toEqual([["text/plain"]]); expect(context.editor.textContent).toBe(expected);
  expect(context.onChange).toHaveBeenCalledExactlyOnceWith({ text_body: expected, html_body: context.editor.innerHTML });
  expect(context.onOtherChange).not.toHaveBeenCalled();
  const s = window.getSelection()!, r = s.getRangeAt(0);
  expect(s.isCollapsed).toBe(true); expect(r.collapsed).toBe(true); expect(context.editor.contains(r.startContainer)).toBe(true);
  const inserted = r.startContainer.childNodes[r.startOffset - 1];
  expect(inserted.nodeType).toBe(Node.TEXT_NODE); expect(inserted.textContent).toBe(pasted);
  expect(s.anchorNode).toBe(r.startContainer); expect(s.anchorOffset).toBe(r.startOffset);
  expect(screen.getByTestId("before")).toHaveTextContent("before-untouched");
  expect(screen.getByTestId("after")).toHaveTextContent("after-untouched");
}

describe("rich message paste Range ownership", () => {
  it.each(["after-forward", "after-backward", "before-backward", "before-forward"])("rejects a genuine %s boundary crossing without mutating any DOM", direction => {
    const context = mount(), inside = context.editor.firstChild!;
    const outside = direction.startsWith("after") ? context.after : context.before;
    const anchorInside = direction === "after-forward" || direction === "before-backward";
    const range = anchorInside ? select(inside, 3, outside, 3) : select(outside, 3, inside, 3);
    expect(context.editor.contains(range.startContainer)).not.toBe(context.editor.contains(range.endContainer));
    expect(context.editor.contains(range.commonAncestorContainer)).toBe(false);
    expect(range.startContainer).toBe(direction.startsWith("after") ? inside : outside);
    expect(range.endContainer).toBe(direction.startsWith("after") ? outside : inside);
    reject(context);
  });
  it.each(["forward", "backward"])("rejects a %s selection spanning two editors", direction => {
    const context = mount("abcdef", undefined, true), current = context.editor.firstChild!, other = context.other!.firstChild!;
    const range = direction === "forward" ? select(current, 2, other, 4) : select(other, 4, current, 2);
    expect(range.startContainer).toBe(current); expect(range.endContainer).toBe(other);
    expect(context.editor.contains(range.commonAncestorContainer)).toBe(false); reject(context);
  });
  it("does not use a selection wholly within another editor", () => {
    const context = mount("abcdef", undefined, true); select(context.other!.firstChild!, 1, context.other!.firstChild!, 4); reject(context);
  });
  it("does not insert into an outside caret", () => { const context = mount(); select(context.before, 2); reject(context); });
  it("does not insert when there is no selection", () => { const context = mount(); window.getSelection()!.removeAllRanges(); reject(context); });
  it.each(["caret", "forward", "backward"])("does not change DOM or emit for a %s selection after disabled is committed", direction => {
    const context = mount(); context.setDisabled(true); expect(context.editor).toHaveAttribute("contenteditable", "false");
    const node = context.editor.firstChild!;
    if (direction === "caret") select(node, 3); else if (direction === "forward") select(node, 1, node, 4); else select(node, 4, node, 1);
    reject(context);
  });
  it("resumes normal paste when the same editor is enabled again", () => {
    const context = mount(); context.setDisabled(true); select(context.editor.firstChild!, 3); reject(context);
    context.setDisabled(false); expect(context.editor).toHaveAttribute("contenteditable", "true");
    select(context.editor.firstChild!, 3); accepted(context, "X", "abcXdef");
  });
});

describe("contained paste controls", () => {
  it.each([["start", "abcdef", 0, "Xabcdef"], ["middle", "abcdef", 3, "abcXdef"], ["end", "abcdef", 6, "abcdefX"], ["empty", "", 0, "X"]] as const)("inserts at the %s caret and leaves it immediately after pasted text", (_label, original, offset, expected) => {
    const context = mount(original); select(context.editor.firstChild ?? context.editor, offset); accepted(context, "X", expected);
  });
  it.each(["forward", "backward"])("replaces a contained %s selection once", direction => {
    const context = mount(), node = context.editor.firstChild!;
    const range = direction === "forward" ? select(node, 1, node, 4) : select(node, 4, node, 1);
    expect(context.editor.contains(range.startContainer)).toBe(true); expect(context.editor.contains(range.endContainer)).toBe(true);
    expect(context.editor.contains(range.commonAncestorContainer)).toBe(true); accepted(context, "X", "aXef");
  });
  it.each(["forward", "backward"])("replaces a %s range across nested formatting nodes", direction => {
    const context = mount("abcdef", "<p><strong>abc</strong><em>def</em></p>");
    const first = context.editor.querySelector("strong")!.firstChild!, last = context.editor.querySelector("em")!.firstChild!;
    const range = direction === "forward" ? select(first, 1, last, 2) : select(last, 2, first, 1);
    expect(range.commonAncestorContainer).toBe(context.editor.firstChild); accepted(context, "X", "aXf");
    expect(context.editor.querySelector("strong")).toHaveTextContent("a"); expect(context.editor.querySelector("em")).toHaveTextContent("f");
  });
  it("accepts an editor-root selection of all children", () => {
    const context = mount("abcdef", "<strong>abc</strong><em>def</em>");
    const range = select(context.editor, 0, context.editor, context.editor.childNodes.length);
    expect(range.commonAncestorContainer).toBe(context.editor); accepted(context, "X", "X");
  });
  it("accepts a caret between editor-root children", () => {
    const context = mount("abcdef", "<strong>abc</strong><em>def</em>"); select(context.editor, 1); accepted(context, "X", "abcXdef");
  });
  it("inserts clipboard markup and line breaks as literal text and emits sanitized HTML", () => {
    const context = mount("AB"); select(context.editor.firstChild!, 1);
    const text = '<img src="https://literal.example.test">\n<&>'; accepted(context, text, `A${text}B`);
    expect(context.editor.querySelector("img, script, iframe, a")).toBeNull();
    expect(context.onChange.mock.calls[0][0].html_body).toContain("&lt;img");
    expect(context.onChange.mock.calls[0][0].html_body).toContain("&lt;&amp;&gt;");
  });
  it("preserves the existing empty-clipboard replacement behavior", () => {
    const context = mount(); select(context.editor.firstChild!, 1, context.editor.firstChild!, 4); accepted(context, "", "aef");
  });
});
