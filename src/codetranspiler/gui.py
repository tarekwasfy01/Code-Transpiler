from __future__ import annotations

import re
import threading
import tkinter as tk
from tkinter import filedialog, messagebox, ttk

from .api import languages, transpile


KEYWORDS = {
    "python": "and as assert async await break class continue def del elif else except False finally for from global if import in is lambda None nonlocal not or pass raise return True try while with yield match case",
    "go": "break default func interface select case defer go map struct chan else goto package switch const fallthrough if range type continue for import return var true false nil",
    "rust": "as break const continue crate else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while async await dyn",
    "c": "auto break case char const continue default do double else enum extern float for goto if inline int long register restrict return short signed sizeof static struct switch typedef union unsigned void volatile while",
    "cpp": "alignas alignof and asm auto bool break case catch char class const constexpr continue default delete do double else enum explicit export extern false float for friend goto if inline int long namespace new noexcept nullptr operator private protected public register reinterpret_cast return short signed sizeof static struct switch template this throw true try typedef typename union unsigned using virtual void volatile wchar_t while",
    "java": "abstract assert boolean break byte case catch char class const continue default do double else enum extends final finally float for goto if implements import instanceof int interface long native new package private protected public return short static strictfp super switch synchronized this throw throws transient try void volatile while true false null",
    "csharp": "abstract as base bool break byte case catch char checked class const continue decimal default delegate do double else enum event explicit extern false finally fixed float for foreach goto if implicit in int interface internal is lock long namespace new null object operator out override params private protected public readonly ref return sbyte sealed short sizeof stackalloc static string struct switch this throw true try typeof uint ulong unchecked unsafe ushort using virtual void volatile while async await var record",
    "javascript": "break case catch class const continue debugger default delete do else export extends false finally for function if import in instanceof let new null return super switch this throw true try typeof var void while with yield async await",
    "se": "semantic program module import export function fn let var const type struct class return if else for while match case call assign binding expression effect io print true false null origin universal_ast relations primitive",
}

GENERIC_KEYWORDS = "if else for while return import package module function fn class struct type let var const true false null nil"


class HighlightText(tk.Text):
    def __init__(self, master, language_var: tk.StringVar, **kwargs):
        super().__init__(master, **kwargs)
        self.language_var = language_var
        self._highlight_job = None
        self.tag_configure("keyword", foreground="#569CD6")
        self.tag_configure("string", foreground="#CE9178")
        self.tag_configure("comment", foreground="#6A9955")
        self.tag_configure("number", foreground="#B5CEA8")
        self.tag_configure("semantic", foreground="#C586C0")
        self.bind("<<Modified>>", self._on_modified)
        self.language_var.trace_add("write", lambda *_: self.schedule_highlight())

    def _on_modified(self, _event=None):
        if self.edit_modified():
            self.edit_modified(False)
            self.schedule_highlight()

    def schedule_highlight(self):
        if self._highlight_job is not None:
            self.after_cancel(self._highlight_job)
        self._highlight_job = self.after(90, self.highlight)

    def highlight(self):
        self._highlight_job = None
        text = self.get("1.0", "end-1c")
        for tag in ("keyword", "string", "comment", "number", "semantic"):
            self.tag_remove(tag, "1.0", "end")
        if not text:
            return
        lang = self.language_var.get().lower()
        keyword_text = KEYWORDS.get(lang, GENERIC_KEYWORDS)
        keywords = set(keyword_text.split())

        # Strings first; comment matching then avoids most false keyword hits.
        spans: list[tuple[int, int, str]] = []
        for m in re.finditer(r'(?s)("(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'|`(?:\\.|[^`\\])*`)', text):
            spans.append((m.start(), m.end(), "string"))
        comment_pattern = r"(?m)//.*$|#.*$|/\*.*?\*/" if lang != "python" else r"(?m)#.*$"
        for m in re.finditer(comment_pattern, text, re.S if "/*" in comment_pattern else 0):
            spans.append((m.start(), m.end(), "comment"))
        for m in re.finditer(r"\b(?:0x[0-9A-Fa-f]+|\d+(?:\.\d+)?)\b", text):
            spans.append((m.start(), m.end(), "number"))
        if keywords:
            pat = r"\b(?:" + "|".join(re.escape(k) for k in sorted(keywords, key=len, reverse=True)) + r")\b"
            for m in re.finditer(pat, text):
                spans.append((m.start(), m.end(), "semantic" if lang == "se" else "keyword"))

        for start, end, tag in spans:
            self.tag_add(tag, f"1.0+{start}c", f"1.0+{end}c")


class App(tk.Tk):
    def __init__(self) -> None:
        super().__init__()
        self.title("Code Transpiler - Python/Tkinter")
        self.geometry("1200x760")
        self.minsize(900, 600)

        ids = [x["id"] for x in languages()]
        self.source = tk.StringVar(value="python")
        self.target = tk.StringVar(value="go")
        self.status = tk.StringVar(value="Ready")

        top = ttk.Frame(self, padding=8)
        top.pack(fill="x")
        ttk.Label(top, text="Source:").pack(side="left")
        ttk.Combobox(top, textvariable=self.source, values=ids, width=12, state="readonly").pack(side="left", padx=(5, 14))
        ttk.Label(top, text="Target:").pack(side="left")
        ttk.Combobox(top, textvariable=self.target, values=ids, width=12, state="readonly").pack(side="left", padx=(5, 14))
        ttk.Button(top, text="Convert", command=self.convert).pack(side="left")
        ttk.Button(top, text="Open", command=self.open_file).pack(side="left", padx=(8, 0))
        ttk.Button(top, text="Save Output", command=self.save_output).pack(side="left", padx=(8, 0))
        ttk.Button(top, text="Swap", command=self.swap).pack(side="left", padx=(8, 0))

        pane = ttk.Panedwindow(self, orient="horizontal")
        pane.pack(fill="both", expand=True, padx=8, pady=(0, 8))

        left = ttk.Labelframe(pane, text="Input", padding=4)
        right = ttk.Labelframe(pane, text="Output", padding=4)
        pane.add(left, weight=1)
        pane.add(right, weight=1)

        self.input_text = HighlightText(left, self.source, wrap="none", undo=True, font=("Consolas", 11))
        self.output_text = HighlightText(right, self.target, wrap="none", undo=True, font=("Consolas", 11))
        self.input_text.pack(fill="both", expand=True)
        self.output_text.pack(fill="both", expand=True)
        self.input_text.insert("1.0", "x = 2\nprint(x + 3)\n")
        self.input_text.schedule_highlight()

        status = ttk.Label(self, textvariable=self.status, anchor="w", padding=(8, 3))
        status.pack(fill="x")

    def swap(self) -> None:
        a, b = self.source.get(), self.target.get()
        self.source.set(b)
        self.target.set(a)
        out = self.output_text.get("1.0", "end-1c")
        if out:
            self.input_text.delete("1.0", "end")
            self.input_text.insert("1.0", out)
        self.input_text.schedule_highlight()
        self.output_text.schedule_highlight()

    def open_file(self) -> None:
        path = filedialog.askopenfilename(title="Open source file")
        if not path:
            return
        try:
            text = open(path, "r", encoding="utf-8").read()
        except Exception as exc:
            messagebox.showerror("Open failed", str(exc))
            return
        self.input_text.delete("1.0", "end")
        self.input_text.insert("1.0", text)
        self.input_text.schedule_highlight()
        self.status.set(path)

    def save_output(self) -> None:
        path = filedialog.asksaveasfilename(title="Save transpiled source")
        if not path:
            return
        try:
            open(path, "w", encoding="utf-8", newline="\n").write(self.output_text.get("1.0", "end-1c"))
            self.status.set(f"Saved: {path}")
        except Exception as exc:
            messagebox.showerror("Save failed", str(exc))

    def convert(self) -> None:
        source = self.source.get()
        target = self.target.get()
        code = self.input_text.get("1.0", "end-1c")
        self.status.set(f"Converting {source} -> {target} ...")

        def worker() -> None:
            try:
                result = transpile(source, target, code)
            except Exception as exc:
                self.after(0, lambda: self._show_error(exc))
                return
            self.after(0, lambda: self._show_result(result, source, target))

        threading.Thread(target=worker, daemon=True).start()

    def _show_result(self, result: str, source: str, target: str) -> None:
        self.output_text.delete("1.0", "end")
        self.output_text.insert("1.0", result)
        self.output_text.schedule_highlight()
        self.status.set(f"Done: {source} -> {target} ({len(result)} chars)")

    def _show_error(self, exc: Exception) -> None:
        self.status.set("Error")
        messagebox.showerror("Transpilation failed", str(exc))


def main() -> None:
    App().mainloop()


if __name__ == "__main__":
    main()
