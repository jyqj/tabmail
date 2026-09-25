import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { cleanEditorHTML, RichMessage } from "./rich-message";
vi.mock("@/components/company/common", () => ({ useText: () => (_zh: string, en: string) => en, inputClass: "", ActionButton: ({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) => <button {...props}>{children}</button> }));
describe("constrained formatting editor", () => {
    it("removes active tags, remote media, attributes and javascript URLs", () => {
        const result = cleanEditorHTML('<p onclick="evil()">Hello <strong>safe</strong><img src="https://tracker.test/x"><script>evil()</script><a href="javascript:evil()">bad</a><a href="https://example.test" onmouseover="evil()">good</a></p>');
        expect(result).toContain("<strong>safe</strong>");
        expect(result).not.toMatch(/onclick|onmouseover|javascript:|<img|<script|tracker/);
        expect(result).toContain('href="https://example.test"');
    });
    it("keeps plain text first-class and blocks HTML drops in rich mode", () => {
        const change = vi.fn();
        render(<RichMessage id="body" text="original" onChange={change}/>);
        fireEvent.change(screen.getByRole("textbox"), { target: { value: "plain edit" } });
        expect(change).toHaveBeenLastCalledWith({ text_body: "plain edit", html_body: undefined });
        fireEvent.click(screen.getByRole("button", { name: "Formatting editor" }));
        const editor = screen.getByRole("textbox", { name: "Formatted message" });
        const drop = new Event("drop", { bubbles: true, cancelable: true });
        editor.dispatchEvent(drop);
        expect(drop.defaultPrevented).toBe(true);
    });
});
