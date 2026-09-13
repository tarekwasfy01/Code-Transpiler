// Copyright (c) 2026 Tarek Wasfy
package ui

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/oligo/gvcode"
	gvcolor "github.com/oligo/gvcode/color"
	"github.com/oligo/gvcode/textstyle/syntax"
	gvwidget "github.com/oligo/gvcode/widget"

	codetranspiler "github.com/tarekwasfy01/Code-Transpiler"
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/highlight"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
	"github.com/tarekwasfy01/Code-Transpiler/internal/platform"
	"github.com/tarekwasfy01/Code-Transpiler/internal/targetrun"
	"github.com/tarekwasfy01/Code-Transpiler/internal/thirdpartylicenses"
)

// GUITranspileExternalProcesses is deliberately false: normal Convert uses
// only the in-process frontend/UAST/backend pipeline. External toolchains are
// reachable only through explicit Run/Compile/Validate actions.
const GUITranspileExternalProcesses = false

type conversionResult struct {
	generation uint64
	code       string
	tokens     []syntax.Token
	err        error
}
type runResult struct {
	generation uint64
	output     string
	err        error
}
type saveResult struct {
	path string
	err  error
}

type languageChoice struct {
	ID        string
	Name      string
	Extension string
}

var uiLanguages = func() []languageChoice {
	out := []languageChoice{{ID: "r", Name: "R", Extension: ".R"}}
	for _, l := range backend.Languages {
		out = append(out, languageChoice{ID: l.ID, Name: l.Name, Extension: l.Extension})
	}
	// Assembly is a first-class machine-language input/output choice.  MASM
	// uses the same lifting boundary; its syntax is accepted by the assembly
	// frontend and is emitted through the NASM-backed native path.
	out = append(out,
		languageChoice{ID: "assembly", Name: "Assembly", Extension: ".asm"},
		languageChoice{ID: "masm", Name: "MASM", Extension: ".asm"},
	)
	// Semantic is represented by one visible, readable language choice.  The
	// parser remains transport-compatible with .sp, .spz and JSON by content,
	// so hiding those legacy/container variants does not remove capability.
	out = append(out, languageChoice{ID: "se", Name: "Semantic", Extension: ".se"})
	return out
}()

type App struct {
	window      *app.Window
	theme       *material.Theme
	hl          *highlight.Service
	left, right *gvcode.Editor

	convertBtn, copyBtn, saveBtn, executableBtn, compilerBtn, nativeCompilerBtn, llvmCompilerBtn, gccCompilerBtn, msvcCompilerBtn, nasmCompilerBtn, masmCompilerBtn, cscCompilerBtn, goCompilerBtn, infoBtn, licensesBtn, copyInfoBtn, closeInfoBtn, openCMDBtn, setPathBtn, modulePathBtn, runBtn widget.Clickable
	fileMenuBtn, editMenuBtn, runMenuBtn, cmdMenuBtn, settingsMenuBtn, modulesMenuBtn, helpMenuBtn                                                                                                                                                                                                 widget.Clickable
	sourceBtn, targetBtn                                                                                                                                                                                                                                                                           widget.Clickable
	sourceClicks, targetClicks                                                                                                                                                                                                                                                                     []widget.Clickable
	sourceOpen, targetOpen                                                                                                                                                                                                                                                                         bool
	source, target                                                                                                                                                                                                                                                                                 int

	showInfo         bool
	infoText         string
	infoScroll       widget.List
	showRun          bool
	runOutput        string
	status           string
	busy             bool
	runtimeFallback  widget.Bool
	saveCompiler     string
	showCompilerMenu bool
	activeRibbonMenu string
	embedModules     widget.Bool
	copyLicenses     widget.Bool
	treeVisible      bool
	treeStarted      time.Time

	convertGeneration atomic.Uint64
	runGeneration     atomic.Uint64
	leftGeneration    atomic.Uint64
	rightGeneration   atomic.Uint64
	cancelConvert     context.CancelFunc
	cancelRun         context.CancelFunc

	convertResults chan conversionResult
	runResults     chan runResult
	saveResults    chan saveResult
}

func New() *App {
	if runtime.GOMAXPROCS(0) < 4 {
		runtime.GOMAXPROCS(4)
	}
	th := material.NewTheme()
	w := &app.Window{}
	w.Option(app.Title("Code Transpiler - Semantic Programming Language"), app.Size(unit.Dp(1280), unit.Dp(760)), app.MinSize(unit.Dp(900), unit.Dp(560)))
	a := &App{
		window: w, theme: th, status: "Ready", saveCompiler: "native",
		convertResults:  make(chan conversionResult, 4),
		runResults:      make(chan runResult, 2),
		saveResults:     make(chan saveResult, 2),
		sourceClicks:    make([]widget.Clickable, len(uiLanguages)),
		targetClicks:    make([]widget.Clickable, len(uiLanguages)),
		source:          0,
		target:          1,
		runtimeFallback: widget.Bool{Value: true},
		infoText:        cliHelp,
		embedModules:    widget.Bool{Value: true},
		copyLicenses:    widget.Bool{Value: true},
	}
	// Initialize the configured Semantic module store on GUI startup. A custom
	// persisted base is honored; otherwise LOCALAPPDATA is used.
	if _, err := backend.DefaultSemanticModuleStore(); err != nil {
		a.status = "Semantic module store: " + err.Error()
	}
	a.infoScroll.List.Axis = layout.Vertical
	a.hl = highlight.NewService(w.Invalidate)
	a.left = newCodeEditor(th, false, codeColorScheme(true, true))
	// The output pane is also an editable Semantic/source workspace.  Convert
	// still replaces it, while Compile/Save can operate on user-authored text.
	a.right = newCodeEditor(th, false, codeColorScheme(true, true))
	const initialR = "# Enter R code here\nx <- c(1, 2, 3)\nprint(x * 2)\n"
	a.left.SetText(initialR)
	a.right.SetText("// Go output will appear here.\n")
	if toks, err := highlight.Tokens(context.Background(), highlight.R, initialR); err == nil {
		a.left.SetSyntaxTokens(toks...)
	}
	if toks, err := highlight.Tokens(context.Background(), highlight.Go, "// Go output will appear here.\n"); err == nil {
		a.right.SetSyntaxTokens(toks...)
	}
	a.scheduleHighlight("left", a.left, a.langForSource(), a.leftGeneration.Add(1))
	a.scheduleHighlight("right", a.right, highlight.Go, a.rightGeneration.Add(1))
	return a
}
func (a *App) Close() {
	if a.cancelConvert != nil {
		a.cancelConvert()
	}
	if a.cancelRun != nil {
		a.cancelRun()
	}
	a.hl.Close()
}
func (a *App) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	platform.BoostGUIThread()
	defer a.Close()
	var ops op.Ops
	for {
		e := a.window.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			a.applyBackgroundResults()
			a.handleEditorEvents(gtx)
			a.handleClicks(gtx)
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}
func newCodeEditor(th *material.Theme, readOnly bool, scheme syntax.ColorScheme) *gvcode.Editor {
	ed := gvwidget.NewEditor(th)
	ed.WithOptions(
		gvcode.WithFont(font.Font{Typeface: "monospace", Weight: font.Bold}),
		gvcode.WithTextSize(unit.Sp(14)),
		gvcode.WithLineHeight(0, 1.35),
		gvcode.WithTabWidth(4), gvcode.WithSoftTab(true), gvcode.WrapLine(false),
		gvcode.WithDefaultGutters(), gvcode.WithGutterGap(unit.Dp(10)), gvcode.WithCornerRadius(unit.Dp(3)),
		gvcode.WithColorScheme(scheme), gvcode.ReadOnlyMode(readOnly),
	)
	return ed
}
func codeColorScheme(colors, blackText bool) syntax.ColorScheme {
	c := syntax.ColorScheme{Name: "r2many-high-contrast-light"}
	fg := "#000000"
	if !blackText {
		fg = "#4B5563"
	}
	c.Foreground = mustColor(fg + "FF")
	c.Background = mustColor("#FFFFFFFF")
	c.SelectColor = mustColor("#94C5FFFF")
	c.LineColor = mustColor("#E5F0FFFF")
	c.LineNumberColor = mustColor(fg + "FF")
	if !colors {
		for _, scope := range []string{"keyword", "name.function", "name.builtin", "name.class", "literal.string", "literal.number", "comment", "operator", "punctuation"} {
			c.AddStyle(syntax.StyleScope(scope), 0, mustColor(fg+"FF"), gvcolor.Color{})
		}
		return c
	}
	c.AddStyle("keyword", syntax.Bold, mustColor("#003CFFFF"), gvcolor.Color{})
	c.AddStyle("name.function", syntax.Bold, mustColor("#7A00CCFF"), gvcolor.Color{})
	c.AddStyle("name.builtin", syntax.Bold, mustColor("#007A3DFF"), gvcolor.Color{})
	c.AddStyle("name.class", syntax.Bold, mustColor("#A03A00FF"), gvcolor.Color{})
	c.AddStyle("literal.string", syntax.Bold, mustColor("#008000FF"), gvcolor.Color{})
	c.AddStyle("literal.number", syntax.Bold, mustColor("#B000B0FF"), gvcolor.Color{})
	c.AddStyle("comment", syntax.Bold, mustColor("#000000FF"), gvcolor.Color{})
	c.AddStyle("operator", syntax.Bold, mustColor("#D00020FF"), gvcolor.Color{})
	c.AddStyle("punctuation", syntax.Bold, mustColor("#000000FF"), gvcolor.Color{})
	return c
}
func mustColor(hex string) gvcolor.Color {
	c, err := gvcolor.Hex2Color(hex)
	if err != nil {
		panic(err)
	}
	return c
}
func (a *App) currentSource() languageChoice { return uiLanguages[a.source] }
func (a *App) currentTarget() languageChoice { return uiLanguages[a.target] }

func highlightLanguage(id string) highlight.Language {
	switch id {
	case "r":
		return highlight.R
	case "go":
		return highlight.Go
	case "rust":
		return highlight.Rust
	case "cpp":
		return highlight.Cpp
	case "c":
		return highlight.C
	case "python":
		return highlight.Python
	case "zig":
		return highlight.Zig
	case "julia":
		return highlight.Julia
	case "nim":
		return highlight.Nim
	case "csharp":
		return highlight.CSharp
	case "java":
		return highlight.Java
	case "kotlin":
		return highlight.Kotlin
	case "swift":
		return highlight.Swift
	default:
		return highlight.Go
	}
}

// parseSemanticGUI accepts every semantic transport the CLI accepts.  The GUI
// intentionally exposes only .se, while existing .sp/.spz/JSON content can be
// pasted or loaded without selecting a different parser.
func parseSemanticGUI(data []byte) (*backend.SemanticProgram, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "SPZ2") {
		return backend.ParseSemanticSPZ(data)
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return backend.ParseSemanticJSON(data)
	}
	return backend.ParseSemanticSE(data)
}
func (a *App) langForSource() highlight.Language { return highlightLanguage(a.currentSource().ID) }
func (a *App) langForTarget() highlight.Language { return highlightLanguage(a.currentTarget().ID) }
func (a *App) handleEditorEvents(gtx layout.Context) {
	for {
		evt, ok := a.left.Update(gtx)
		if !ok {
			break
		}
		if _, changed := evt.(gvcode.ChangeEvent); changed {
			a.scheduleHighlight("left", a.left, a.langForSource(), a.leftGeneration.Add(1))
		}
	}
	for {
		_, ok := a.right.Update(gtx)
		if !ok {
			break
		}
	}
}
func (a *App) scheduleHighlight(tag string, ed *gvcode.Editor, lang highlight.Language, generation uint64) {
	a.hl.Submit(highlight.Request{Tag: tag, Generation: generation, Language: lang, Reader: ed.GetReader()})
}
func (a *App) applyBackgroundResults() {
	for {
		select {
		case res := <-a.convertResults:
			if res.generation != a.convertGeneration.Load() {
				continue
			}
			a.busy = false
			a.treeVisible = true
			// A completed conversion leaves the graph fully illuminated.
			if res.err == nil {
				a.treeStarted = time.Time{}
			}
			if strings.TrimSpace(res.code) != "" {
				a.right.SetText(res.code)
				a.rightGeneration.Add(1)
				a.right.SetSyntaxTokens(res.tokens...)
			}
			if res.err != nil {
				a.status = "Convert failed: " + res.err.Error()
			} else {
				a.status = "Converted " + a.currentSource().Name + " → " + a.currentTarget().Name
			}
		case res := <-a.runResults:
			if res.generation != a.runGeneration.Load() {
				continue
			}
			a.busy = false
			a.showRun = true
			a.runOutput = res.output
			if res.err != nil {
				if a.runOutput != "" {
					a.runOutput += "\n"
				}
				a.runOutput += "ERROR: " + res.err.Error()
				a.status = "Runtime error"
			} else {
				a.status = "Run finished"
			}
		case res := <-a.saveResults:
			a.busy = false
			if res.err != nil {
				a.status = "Save failed: " + res.err.Error()
			} else if res.path != "" {
				a.status = "Saved: " + res.path
			} else {
				a.status = "Save cancelled"
			}
		case res := <-a.hl.Results():
			if res.Err != nil {
				continue
			}
			switch res.Tag {
			case "left":
				if res.Generation == a.leftGeneration.Load() {
					a.left.SetSyntaxTokens(res.Tokens...)
				}
			case "right":
				if res.Generation == a.rightGeneration.Load() {
					a.right.SetSyntaxTokens(res.Tokens...)
				}
			}
		default:
			return
		}
	}
}
func (a *App) handleClicks(gtx layout.Context) {
	for name, btn := range map[string]*widget.Clickable{
		"file": &a.fileMenuBtn, "edit": &a.editMenuBtn, "run": &a.runMenuBtn,
		"cmd": &a.cmdMenuBtn, "settings": &a.settingsMenuBtn, "modules": &a.modulesMenuBtn, "help": &a.helpMenuBtn,
	} {
		if btn.Clicked(gtx) {
			if a.activeRibbonMenu == name {
				a.activeRibbonMenu = ""
			} else {
				a.activeRibbonMenu = name
			}
			if name == "help" {
				a.showInfo = true
				a.infoText = cliHelp + "\n\nMANUAL\nFile: New, Load file, Save file, Save As.\nEdit: Undo, Redo, Cut, Copy, Paste, Find/Replace, refresh syntax highlighting.\nRun: Run or Run with console; Convert and Save Executable.\nCmd: open a terminal with the CLI and show command help.\nSettings: toggle runtime fallback, imported-module embedding, and package-license copying.\nModules: manage the Semantic module store, acquire packages, and update or reinstall them."
			}
		}
	}
	if a.convertBtn.Clicked(gtx) {
		a.startConvert()
	}
	if a.runBtn.Clicked(gtx) {
		a.startRun()
	}
	if a.copyBtn.Clicked(gtx) {
		gtx.Execute(clipboard.WriteCmd{Type: "text/plain", Data: io.NopCloser(a.right.GetReader())})
		a.status = "Copied " + a.currentTarget().Name + " code"
	}
	if a.saveBtn.Clicked(gtx) {
		a.startSaveAs(a.right.GetReader())
	}
	if a.executableBtn.Clicked(gtx) {
		a.startSaveExecutable()
	}
	if a.compilerBtn.Clicked(gtx) {
		a.showCompilerMenu = !a.showCompilerMenu
	}
	if a.nativeCompilerBtn.Clicked(gtx) {
		a.saveCompiler = "native"
		a.showCompilerMenu = false
	}
	if a.llvmCompilerBtn.Clicked(gtx) {
		a.saveCompiler = "llvm"
		a.showCompilerMenu = false
	}
	if a.gccCompilerBtn.Clicked(gtx) {
		a.saveCompiler = "gcc"
		a.showCompilerMenu = false
	}
	if a.msvcCompilerBtn.Clicked(gtx) {
		a.saveCompiler = "msvc"
		a.showCompilerMenu = false
	}
	if a.nasmCompilerBtn.Clicked(gtx) {
		a.saveCompiler = "nasm"
		a.showCompilerMenu = false
	}
	if a.masmCompilerBtn.Clicked(gtx) {
		a.saveCompiler = "masm"
		a.showCompilerMenu = false
	}
	if a.cscCompilerBtn.Clicked(gtx) {
		a.saveCompiler = "csc"
		a.showCompilerMenu = false
	}
	if a.goCompilerBtn.Clicked(gtx) {
		a.saveCompiler = "go"
		a.showCompilerMenu = false
	}
	if a.infoBtn.Clicked(gtx) {
		a.showInfo = !a.showInfo
		a.infoText = cliHelp
	}
	if a.licensesBtn.Clicked(gtx) {
		a.showInfo = true
		a.infoText = cliHelp + "\n\n" + thirdpartylicenses.Summary()
	}
	if a.closeInfoBtn.Clicked(gtx) {
		a.showInfo = false
	}
	if a.copyInfoBtn.Clicked(gtx) {
		gtx.Execute(clipboard.WriteCmd{Type: "text/plain", Data: io.NopCloser(strings.NewReader(a.infoText))})
		a.status = "Copied CLI and license information"
	}
	if a.openCMDBtn.Clicked(gtx) {
		exe, _ := os.Executable()
		if err := platform.OpenCMD(exe); err != nil {
			a.status = "Open CMD failed: " + err.Error()
		}
	}
	if a.setPathBtn.Clicked(gtx) {
		exe, err := os.Executable()
		if err != nil {
			a.status = "Set PATH failed: " + err.Error()
		} else if err = platform.SetPath(exe); err != nil {
			a.status = "Set PATH failed: " + err.Error()
		} else {
			a.status = "PATH update requested (UAC)"
		}
	}
	if a.modulePathBtn.Clicked(gtx) {
		path, err := platform.SelectFolderDialog("Semantic module storage parent folder")
		if err != nil {
			a.status = "Module path failed: " + err.Error()
		} else if path != "" {
			if err = backend.SetSemanticModuleBase(path); err != nil {
				a.status = "Module path failed: " + err.Error()
			} else {
				a.status = "Module path: " + path + "\\Semantic"
			}
		}
	}
	if a.sourceBtn.Clicked(gtx) {
		a.sourceOpen = !a.sourceOpen
		a.targetOpen = false
	}
	if a.targetBtn.Clicked(gtx) {
		a.targetOpen = !a.targetOpen
		a.sourceOpen = false
	}
	for i := range a.sourceClicks {
		if a.sourceClicks[i].Clicked(gtx) {
			a.source = i
			a.sourceOpen = false
			a.status = "Input: " + a.currentSource().Name
			a.scheduleHighlight("left", a.left, a.langForSource(), a.leftGeneration.Add(1))
		}
	}
	for i := range a.targetClicks {
		if a.targetClicks[i].Clicked(gtx) {
			a.target = i
			a.targetOpen = false
			a.status = "Output: " + a.currentTarget().Name
			a.scheduleHighlight("right", a.right, a.langForTarget(), a.rightGeneration.Add(1))
		}
	}
}
func (a *App) startConvert() {
	if a.cancelConvert != nil {
		a.cancelConvert()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelConvert = cancel
	gen := a.convertGeneration.Add(1)
	a.busy = true
	a.treeVisible = true
	a.treeStarted = time.Now()
	a.status = "Converting " + a.currentSource().Name + " → " + a.currentTarget().Name + "…"
	reader := a.left.GetReader()
	// Snapshot editor bytes on the UI thread. The worker must never retain a
	// mutable TextWidget reader while the user is typing or clicks Convert
	// again; each request owns its immutable source and language pair.
	data, readErr := io.ReadAll(reader)
	source := a.currentSource().ID
	target := a.currentTarget().ID
	lang := a.langForTarget()
	disableRuntime := !a.runtimeFallback.Value
	go func() {
		err := readErr
		code := ""
		var toks []syntax.Token
		if err == nil {
			if source == "assembly" || source == "masm" {
				// Assembly/MASM input is lifted through the shared binary frontend;
				// no textual language frontend is involved.
				if target == "se" || target == "sp" || target == "spz" || target == "semantic" {
					p, e := backend.LiftBinaryInput(data, backend.CompileOptions{InputKind: backend.CompileInputAssembly, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
					if e != nil {
						err = e
					} else if target == "spz" {
						var z []byte
						z, err = p.MarshalSemanticSPZ()
						code = string(z)
					} else if target == "sp" {
						var s []byte
						s, err = p.MarshalSemanticSP()
						code = string(s)
					} else {
						var s []byte
						// The GUI's Semantic language is the human-readable .se
						// representation. Keep SP as the explicit legacy transport,
						// but never expose compact/legacy SP when .se is selected.
						s, err = p.MarshalSemanticSEReadable()
						code = string(s)
					}
				} else if target == "assembly" || target == "masm" {
					code = string(data)
				} else {
					result, e := manytomany.TranspileCore(manytomany.TranspileRequest{Source: string(data), SourceLanguage: "assembly", TargetLanguage: target, EntryPoint: "gui"})
					code, err = result.Code, e
				}
			} else if target == "assembly" || target == "masm" {
				result, e := codetranspiler.Compile(string(data), codetranspiler.CompileOptions{SourceLanguage: source, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64", OutputKind: codetranspiler.Assembly, EntryPoint: "gui"})
				code, err = result.Text, e
			} else if source == "sp" || source == "spz" || source == "se" {
				p, e := parseSemanticGUI(data)
				if e != nil {
					err = e
				} else if target == "se" || target == "sp" || target == "spz" {
					if target == "spz" {
						var z []byte
						z, err = p.MarshalSemanticSPZ()
						code = string(z)
					} else if target == "sp" {
						var s []byte
						s, err = p.MarshalSemanticSP()
						code = string(s)
					} else {
						var s []byte
						// .se is the GUI-facing readable Semantic form.
						s, err = p.MarshalSemanticSEReadable()
						code = string(s)
					}
				} else {
					storeRoot, _ := backend.ModuleStoreRoot()
					code, err = manytomany.TranspileSemanticSPWithOptions(target, data, manytomany.TranspileRequest{
						TargetLanguage: target, EntryPoint: "gui", ModuleBaseDir: filepath.Dir(os.Args[0]), ModuleStoreRoot: storeRoot, EmbedAllModules: a.embedModules.Value, ModuleEmbeddingMode: "all",
					})
				}
			} else if target == "sp" || target == "spz" || target == "se" {
				var sp []byte
				sp, err = manytomany.SemanticSP(source, string(data))
				if err == nil && target == "se" {
					p, e := backend.ParseSemanticSP(sp)
					if e != nil {
						err = e
					} else {
						sp, err = p.MarshalSemanticSEReadable()
					}
				}
				if err == nil && (target == "spz") {
					p, e := backend.ParseSemanticSP(sp)
					if e != nil {
						err = e
					} else {
						var z []byte
						z, err = p.MarshalSemanticSPZ()
						sp = z
					}
				}
				code = string(sp)
			} else {
				storeRoot, _ := backend.ModuleStoreRoot()
				result, convertErr := manytomany.TranspileCore(manytomany.TranspileRequest{Source: string(data), SourceLanguage: source, TargetLanguage: target, EntryPoint: "gui", ModuleBaseDir: filepath.Dir(os.Args[0]), ModuleStoreRoot: storeRoot, EmbedAllModules: a.embedModules.Value, ModuleEmbeddingMode: "all", DisableRuntimeFallback: disableRuntime})
				code, err = result.Code, convertErr
			}
		}
		if err == nil && strings.TrimSpace(code) != "" {
			toks, _ = highlight.Tokens(ctx, lang, code)
		}
		select {
		case a.convertResults <- conversionResult{generation: gen, code: code, tokens: toks, err: err}:
		default:
		}
		a.window.Invalidate()
	}()
}
func (a *App) startRun() {
	if a.cancelRun != nil {
		a.cancelRun()
	}
	_, cancel := context.WithCancel(context.Background())
	a.cancelRun = cancel
	gen := a.runGeneration.Add(1)
	a.busy = true
	a.status = "Running " + a.currentSource().Name + " with internal runtime…"
	reader := a.left.GetReader()
	source := a.currentSource().ID
	go func() {
		data, err := io.ReadAll(reader)
		out := ""
		if err == nil {
			if source == "sp" || source == "spz" || source == "se" {
				p, e := parseSemanticGUI(data)
				if e != nil {
					err = e
				} else {
					out, err = backend.RunSemantic(p)
				}
			} else if source == "semantic" {
				p, e := backend.ParseSemanticJSON(data)
				if e != nil {
					err = e
				} else {
					out, err = backend.RunSemantic(p)
				}
			} else {
				result, e := targetrun.RunSource("embedded", source, string(data))
				out, err = result.Stdout, e
				if result.Stderr != "" {
					out += "\n" + result.Stderr
				}
			}
		}
		select {
		case a.runResults <- runResult{generation: gen, output: out, err: err}:
		default:
		}
		a.window.Invalidate()
	}()
}
func (a *App) startSaveAs(reader io.Reader) {
	a.status = "Choose save location…"
	target := a.currentTarget()
	go func() {
		data, err := io.ReadAll(reader)
		path := ""
		if err == nil {
			path, err = platform.SaveSourceFileDialog("output"+target.Extension, target.Extension, target.Name+" source")
		}
		if err == nil && path != "" {
			err = os.WriteFile(path, data, 0644)
		}
		if err == nil && path != "" && a.copyLicenses.Value {
			_, err = backend.CopyImportedPackageLicenses("", path)
		}
		select {
		case a.saveResults <- saveResult{path: path, err: err}:
		default:
		}
		a.window.Invalidate()
	}()
}

// startSaveExecutable uses the same public Compile path as the CLI.  The
// native bytes are kept binary until the Save As dialog writes them; the
// editor is never used as a transport for an executable.
func (a *App) startSaveExecutable() {
	if a.busy {
		return
	}
	a.busy = true
	a.status = "Building executable…"
	data, err := io.ReadAll(a.right.GetReader())
	source := a.currentTarget().ID
	if strings.TrimSpace(string(data)) == "" || strings.HasPrefix(string(data), "// Go output will appear here") {
		data, err = io.ReadAll(a.left.GetReader())
		source = a.currentSource().ID
	}
	compiler := a.saveCompiler
	if compiler == "" {
		compiler = "native"
	}
	go func() {
		var out codetranspiler.CompileResult
		if err == nil {
			if compiler == "llvm" {
				var program *backend.SemanticProgram
				switch strings.ToLower(source) {
				case "se", "sp":
					program, err = backend.ParseSemanticSE(data)
				case "json":
					program, err = backend.ParseSemanticJSON(data)
				default:
					// Import regular source through the same ModernFrontend used by
					// CLI package imports before handing the canonical program to LLVM.
					program, err = backend.LowerSource(source, "input", string(data))
				}
				if err == nil {
					llvm := codetranspiler.LLVMCompileOptions{OutputKind: codetranspiler.LLVMExecutable, TargetTriple: "x86_64-pc-windows-msvc", EmitEntryWrapper: true}
					if _, statErr := os.Stat(`C:\Program Files\clang+llvm-23.1.1-x86_64-pc-windows-msvc\bin`); statErr == nil {
						llvm.LLVMPath = `C:\Program Files\clang+llvm-23.1.1-x86_64-pc-windows-msvc\bin`
					}
					llvmResult, compileErr := codetranspiler.CompileLLVM(program, llvm)
					if compileErr != nil {
						err = compileErr
					} else {
						out.Bytes = llvmResult.Bytes
					}
				}
			} else if compiler == "gcc" || compiler == "msvc" {
				var program *backend.SemanticProgram
				switch strings.ToLower(source) {
				case "se", "sp":
					program, err = backend.ParseSemanticSE(data)
				case "json":
					program, err = backend.ParseSemanticJSON(data)
				case "go":
					program, err = backend.LowerNativeGo("input.go", string(data))
					if err != nil {
						program, err = backend.LowerSource(source, "input.go", string(data))
					}
				default:
					program, err = backend.LowerSource(source, "input", string(data))
				}
				if err == nil {
					external, compileErr := codetranspiler.CompileExternalC(program, codetranspiler.ExternalCCompileOptions{
						Family: codetranspiler.ExternalCompilerFamily(compiler), OutputKind: codetranspiler.Executable, Optimization: 2,
					})
					if compileErr != nil {
						err = compileErr
					} else {
						out.Bytes = external.Bytes
					}
				}
			} else if compiler == "nasm" {
				out, err = codetranspiler.Compile(string(data), codetranspiler.CompileOptions{SourceLanguage: source, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64", OutputKind: codetranspiler.Executable, ViaAssembly: true})
			} else if compiler == "masm" {
				// MASM is an explicit compiler choice. Do not silently route it
				// through the NASM/native path: MASM syntax and ml64 linking are
				// different contracts. Report the missing integration clearly.
				if _, lookErr := exec.LookPath("ml64.exe"); lookErr != nil {
					err = fmt.Errorf("MASM compiler selected, but ml64.exe was not found in PATH: %w", lookErr)
				} else {
					err = fmt.Errorf("MASM compiler selected, but the ml64.exe assembly/link pipeline is not available yet")
				}
			} else if compiler == "go" {
				var program *backend.SemanticProgram
				switch strings.ToLower(source) {
				case "se", "sp", "spz":
					program, err = parseSemanticGUI(data)
				default:
					program, err = backend.LowerSource(source, "input", string(data))
				}
				if err == nil {
					goSource, e := backend.EmitSemantic("go", program)
					if e != nil {
						err = e
					} else if tmp, e := os.MkdirTemp("", "codetranspiler-go-"); e != nil {
						err = e
					} else {
						defer os.RemoveAll(tmp)
						goPath, exePath := filepath.Join(tmp, "main.go"), filepath.Join(tmp, "program.exe")
						if err = os.WriteFile(goPath, []byte(goSource), 0644); err == nil {
							goExe, lookErr := exec.LookPath("go.exe")
							if lookErr != nil {
								goExe = filepath.Join(runtime.GOROOT(), "bin", "go.exe")
								if _, statErr := os.Stat(goExe); statErr != nil {
									err = fmt.Errorf("Go compiler not found in PATH or GOROOT: %w", lookErr)
								}
							}
							if err == nil {
								cmd := exec.Command(goExe, "build", "-o", exePath, goPath)
								cmd.Dir = tmp
								if b, ce := cmd.CombinedOutput(); ce != nil {
									err = fmt.Errorf("go build failed: %w: %s", ce, strings.TrimSpace(string(b)))
								} else {
									out.Bytes, err = os.ReadFile(exePath)
								}
							}
						}
					}
				}
			} else if compiler == "csc" {
				var program *backend.SemanticProgram
				switch strings.ToLower(source) {
				case "se", "sp", "spz":
					program, err = parseSemanticGUI(data)
				default:
					program, err = backend.LowerSource(source, "input", string(data))
				}
				if err == nil {
					cs, e := backend.EmitSemantic("csharp", program)
					if e != nil {
						err = e
					} else {
						tmp, e := os.MkdirTemp("", "codetranspiler-csc-")
						if e != nil {
							err = e
						} else {
							defer os.RemoveAll(tmp)
							csPath, exePath := filepath.Join(tmp, "program.cs"), filepath.Join(tmp, "program.exe")
							if err = os.WriteFile(csPath, []byte(cs), 0644); err == nil {
								csc, e := findCSCCompiler()
								if e != nil {
									err = fmt.Errorf("csc.exe not found: %w", e)
								} else {
									cmd := exec.Command(csc, "/nologo", "/target:exe", "/out:"+exePath, csPath)
									cmd.Dir = tmp
									if b, ce := cmd.CombinedOutput(); ce != nil {
										err = fmt.Errorf("csc.exe failed: %w: %s", ce, strings.TrimSpace(string(b)))
									} else {
										out.Bytes, err = os.ReadFile(exePath)
									}
								}
							}
						}
					}
				}
			}
		}
		path := ""
		if err == nil {
			path, err = platform.SaveSourceFileDialog("program.exe", ".exe", "Executable")
		}
		if err == nil && path != "" {
			err = os.WriteFile(path, out.Bytes, 0700)
		}
		if err == nil && path != "" && a.copyLicenses.Value {
			_, err = backend.CopyImportedPackageLicenses("", path)
		}
		select {
		case a.saveResults <- saveResult{path: path, err: err}:
		default:
		}
		a.window.Invalidate()
	}()
}

func (a *App) layoutCompilerMenu(gtx layout.Context) layout.Dimensions {
	if !a.showCompilerMenu {
		return layout.Dimensions{}
	}
	return layout.Inset{Top: 2, Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return widget.Border{Color: color.NRGBA{R: 208, G: 215, B: 222, A: 255}, Width: 1, CornerRadius: 7}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 5, Bottom: 5, Left: 7, Right: 7}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return smallButton(gtx, a.theme, &a.nativeCompilerBtn, "Native compiler")
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Height: 3}.Layout(gtx) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return smallButton(gtx, a.theme, &a.llvmCompilerBtn, "LLVM compiler")
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Height: 3}.Layout(gtx) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return smallButton(gtx, a.theme, &a.gccCompilerBtn, "GCC / MinGW")
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Height: 3}.Layout(gtx) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return smallButton(gtx, a.theme, &a.msvcCompilerBtn, "MSVC")
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Height: 3}.Layout(gtx) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return smallButton(gtx, a.theme, &a.nasmCompilerBtn, "NASM / Assembly")
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Height: 3}.Layout(gtx) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return smallButton(gtx, a.theme, &a.masmCompilerBtn, "MASM / ml64.exe")
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Height: 3}.Layout(gtx) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return smallButton(gtx, a.theme, &a.cscCompilerBtn, "C# csc.exe")
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Height: 3}.Layout(gtx) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return smallButton(gtx, a.theme, &a.goCompilerBtn, "Go compiler")
							}),
						)
					})
				})
			}),
		)
	})
}
func (a *App) layout(gtx layout.Context) layout.Dimensions {
	if a.busy {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(250 * time.Millisecond)})
	}
	return layout.Inset{Top: 12, Bottom: 10, Left: 12, Right: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(a.layoutRibbon),
			layout.Rigid(a.layoutHeader),
			layout.Rigid(a.layoutRibbonMenu),
			layout.Rigid(a.layoutCompilerMenu),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !a.sourceOpen && !a.targetOpen {
					return layout.Spacer{Height: 10}.Layout(gtx)
				}
				return a.layoutLanguageMenu(gtx)
			}),
			layout.Flexed(1, a.layoutMain),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !a.showRun {
					return layout.Dimensions{}
				}
				return a.layoutRunOutput(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !a.showInfo {
					return layout.Dimensions{}
				}
				return a.layoutInfo(gtx)
			}),
			layout.Rigid(a.layoutFooter),
		)
	})
}

func (a *App) layoutRibbon(gtx layout.Context) layout.Dimensions {
	items := []struct {
		b     *widget.Clickable
		label string
	}{
		{&a.fileMenuBtn, "File"}, {&a.editMenuBtn, "Edit"}, {&a.runMenuBtn, "Run"},
		{&a.cmdMenuBtn, "Cmd"}, {&a.settingsMenuBtn, "Settings"}, {&a.modulesMenuBtn, "Modules"}, {&a.helpMenuBtn, "Help"},
	}
	children := make([]layout.FlexChild, 0, len(items)*2)
	for i, item := range items {
		if i > 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 4}.Layout(gtx) }))
		}
		it := item
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return smallButton(gtx, a.theme, it.b, it.label+"  ▾") }))
	}
	return widget.Border{Color: color.NRGBA{R: 218, G: 224, B: 230, A: 255}, Width: 1, CornerRadius: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 4, Bottom: 4, Left: 4, Right: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
		})
	})
}

func (a *App) layoutRibbonMenu(gtx layout.Context) layout.Dimensions {
	if a.activeRibbonMenu == "" {
		return layout.Dimensions{}
	}
	labels := map[string][]string{
		"file":     {"New", "Load file", "Save file", "Save As"},
		"edit":     {"Undo", "Redo", "Cut", "Copy", "Paste", "Find / Replace", "Refresh syntax highlighting"},
		"run":      {"Run", "Run with console", "Convert", "Save Executable"},
		"cmd":      {"Open CMD", "CLI help"},
		"settings": {"Runtime fallback", "Embed imported modules", "Include package licenses"},
		"modules":  {"Semantic Modules", "Get package", "Set module folder", "Delete", "Reinstall", "Update"},
		"help":     {"Manual", "Info", "Licenses", "Set PATH"},
	}
	items := labels[a.activeRibbonMenu]
	var noop widget.Clickable
	return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: color.NRGBA{R: 218, G: 224, B: 230, A: 255}, Width: 1, CornerRadius: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 4, Bottom: 4, Left: 5, Right: 5}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				children := make([]layout.FlexChild, 0, len(items)*2)
				for i, label := range items {
					if i > 0 {
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 5}.Layout(gtx) }))
					}
					textLabel := label
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						b := material.Button(a.theme, &noop, textLabel)
						b.Background = color.NRGBA{R: 247, G: 249, B: 251, A: 255}
						b.Color = color.NRGBA{R: 45, G: 52, B: 60, A: 255}
						b.CornerRadius = 5
						b.Inset = layout.Inset{Top: 5, Bottom: 5, Left: 8, Right: 8}
						return b.Layout(gtx)
					}))
				}
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
			})
		})
	})
}
func (a *App) layoutHeader(gtx layout.Context) layout.Dimensions {
	runLabel := "Run " + a.currentSource().Name
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return smallButton(gtx, a.theme, &a.sourceBtn, "Input: "+a.currentSource().Name+"  ▼")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 8}.Layout(gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return smallButton(gtx, a.theme, &a.runBtn, runLabel)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return smallButton(gtx, a.theme, &a.targetBtn, "Output: "+a.currentTarget().Name+"  ▼")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 8}.Layout(gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			label := "Save Executable"
			if a.saveCompiler == "llvm" {
				label = "Save Executable (LLVM)"
			} else if a.saveCompiler == "gcc" {
				label = "Save Executable (GCC)"
			} else if a.saveCompiler == "msvc" {
				label = "Save Executable (MSVC)"
			} else if a.saveCompiler == "nasm" {
				label = "Save Executable (NASM / Assembly)"
			} else if a.saveCompiler == "masm" {
				label = "Save Executable (MASM / ml64.exe)"
			} else if a.saveCompiler == "csc" {
				label = "Save Executable (C# csc.exe)"
			} else if a.saveCompiler == "go" {
				label = "Save Executable (Go compiler)"
			}
			return smallButton(gtx, a.theme, &a.executableBtn, label)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return smallButton(gtx, a.theme, &a.compilerBtn, "▼")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 8}.Layout(gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return smallButton(gtx, a.theme, &a.openCMDBtn, "Open CMD")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 12}.Layout(gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			label := material.Body2(a.theme, a.status)
			label.Alignment = text.End
			label.Color = color.NRGBA{R: 87, G: 96, B: 106, A: 255}
			return label.Layout(gtx)
		}),
	)
}

func (a *App) layoutLanguageMenu(gtx layout.Context) layout.Dimensions {
	clicks := a.targetClicks
	title := "Output language"
	if a.sourceOpen {
		clicks = a.sourceClicks
		title = "Input language"
	}
	row := func(from, to int) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			children := make([]layout.FlexChild, 0, (to-from)*2)
			for i := from; i < to; i++ {
				ii := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return smallButton(gtx, a.theme, &clicks[ii], uiLanguages[ii].Name)
				}))
				if i < to-1 {
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Spacer{Width: 5}.Layout(gtx)
					}))
				}
			}
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
		}
	}
	split := 7
	if split > len(uiLanguages) {
		split = len(uiLanguages)
	}
	return layout.Inset{Top: 6, Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: color.NRGBA{R: 208, G: 215, B: 222, A: 255}, Width: 1, CornerRadius: 7}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 6, Bottom: 6, Left: 6, Right: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						label := material.Caption(a.theme, title)
						label.Font.Weight = font.SemiBold
						return label.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Spacer{Height: 5}.Layout(gtx)
					}),
					layout.Rigid(row(0, split)),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if split >= len(uiLanguages) {
							return layout.Dimensions{}
						}
						return layout.Spacer{Height: 5}.Layout(gtx)
					}),
					layout.Rigid(row(split, len(uiLanguages))),
				)
			})
		})
	})
}
func (a *App) layoutMain(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return a.layoutEditorPanel(gtx, "Input · "+a.currentSource().Name, a.left)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: 10, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(a.theme, &a.convertBtn, "Convert")
						btn.Background = color.NRGBA{R: 9, G: 105, B: 218, A: 255}
						btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
						btn.CornerRadius = unit.Dp(8)
						btn.Inset = layout.Inset{Top: 10, Bottom: 10, Left: 16, Right: 16}
						return btn.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Height: 14}.Layout(gtx) }),
					layout.Rigid(a.layoutTreeOfLife),
				)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return a.layoutEditorPanel(gtx, "Output · "+a.currentTarget().Name, a.right)
		}),
	)
}

// layoutTreeOfLife is a lightweight, dependency-free progress visualization.
// The graph is intentionally drawn with text glyphs so it remains available in
// the portable GUI build. Nodes illuminate from the bottom upward while a
// conversion is running and remain orange after completion.
func (a *App) layoutTreeOfLife(gtx layout.Context) layout.Dimensions {
	if !a.treeVisible {
		return layout.Dimensions{}
	}
	progress := float32(1)
	if a.busy && !a.treeStarted.IsZero() {
		progress = float32(gtx.Now.Sub(a.treeStarted)) / float32(8*time.Second)
		if progress < 0 {
			progress = 0
		}
		if progress > 0.95 {
			progress = 0.95
		}
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(120 * time.Millisecond)})
	}
	levels := []string{"●", "● ●", "● ●", "● ●", "●", "●"}
	return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Caption(a.theme, "Semantic")
			l.Font.Weight = font.SemiBold
			l.Color = color.NRGBA{R: 87, G: 96, B: 106, A: 255}
			return l.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 5, Bottom: 5}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return a.treeGlyph(gtx, levels[0], progress >= 0.85) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return a.treeGlyph(gtx, "│", progress >= 0.68) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return a.treeGlyph(gtx, levels[1], progress >= 0.52) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return a.treeGlyph(gtx, "│", progress >= 0.36) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return a.treeGlyph(gtx, levels[2], progress >= 0.20) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return a.treeGlyph(gtx, "│", progress >= 0.08) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return a.treeGlyph(gtx, levels[5], true) }),
				)
			})
		}),
	)
}

func (a *App) treeGlyph(gtx layout.Context, glyph string, active bool) layout.Dimensions {
	l := material.Body1(a.theme, glyph)
	l.Alignment = text.Middle
	if active {
		l.Color = color.NRGBA{R: 255, G: 145, B: 25, A: 255}
	} else {
		l.Color = color.NRGBA{R: 210, G: 216, B: 222, A: 255}
	}
	l.TextSize = unit.Sp(22)
	return l.Layout(gtx)
}
func (a *App) layoutEditorPanel(gtx layout.Context, title string, ed *gvcode.Editor) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			label := material.Body1(a.theme, title)
			label.Font.Weight = font.SemiBold
			return label.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Height: 8}.Layout(gtx) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return widget.Border{Color: color.NRGBA{R: 208, G: 215, B: 222, A: 255}, Width: unit.Dp(1), CornerRadius: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 6, Bottom: 6, Left: 6, Right: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return ed.Layout(gtx, a.theme.Shaper) })
			})
		}),
	)
}

// findCSCCompiler resolves the C# compiler without requiring a global PATH
// entry. Visual Studio/.NET installations commonly keep csc.exe below either
// Framework or Framework64, so the search is deliberately data-driven.
func findCSCCompiler() (string, error) {
	for _, name := range []string{"csc.exe", "csc"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	for _, path := range []string{
		`C:\Windows\Microsoft.NET\Framework64\v4.0.30319\csc.exe`,
		`C:\Windows\Microsoft.NET\Framework\v4.0.30319\csc.exe`,
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	root := `C:\Windows\Microsoft.NET`
	var found string
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsPermission(err) {
				return filepath.SkipDir
			}
			return err
		}
		if found != "" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.EqualFold(entry.Name(), "csc.exe") {
			found = path
		}
		return nil
	})
	if found != "" {
		return found, nil
	}
	if walkErr != nil {
		return "", walkErr
	}
	return "", fmt.Errorf("searched PATH and %s", root)
}

func (a *App) layoutRunOutput(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: color.NRGBA{R: 208, G: 215, B: 222, A: 255}, Width: 1, CornerRadius: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 8, Bottom: 8, Left: 10, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Caption(a.theme, "Runtime")
						l.Font.Weight = font.SemiBold
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Body2(a.theme, a.runOutput)
						l.Color = color.NRGBA{R: 31, G: 35, B: 40, A: 255}
						return l.Layout(gtx)
					}),
				)
			})
		})
	})
}
func (a *App) layoutInfo(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: color.NRGBA{R: 208, G: 215, B: 222, A: 255}, Width: 1, CornerRadius: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 10, Bottom: 10, Left: 12, Right: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								label := material.Body1(a.theme, "CLI / Info")
								label.Font.Weight = font.SemiBold
								return label.Layout(gtx)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return layout.Dimensions{Size: gtx.Constraints.Min}
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return smallButton(gtx, a.theme, &a.closeInfoBtn, "×")
							}),
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Spacer{Height: 8}.Layout(gtx)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						label := material.Body2(a.theme, a.infoText)
						label.Color = color.NRGBA{R: 31, G: 35, B: 40, A: 255}
						return a.infoScroll.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions { return label.Layout(gtx) })
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return smallButton(gtx, a.theme, &a.copyInfoBtn, "Copy CLI commands")
						})
					}),
				)
			})
		})
	})
}
func (a *App) layoutFooter(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return smallButton(gtx, a.theme, &a.copyBtn, "Copy") }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 8}.Layout(gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return smallButton(gtx, a.theme, &a.saveBtn, "Save As") }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 8}.Layout(gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return smallButton(gtx, a.theme, &a.infoBtn, "Info") }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 8}.Layout(gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return smallButton(gtx, a.theme, &a.licensesBtn, "Licenses")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 8}.Layout(gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return smallButton(gtx, a.theme, &a.setPathBtn, "Set PATH")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 8}.Layout(gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return smallButton(gtx, a.theme, &a.modulePathBtn, "Semantic Modules")
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				check := material.CheckBox(a.theme, &a.runtimeFallback, "Semantic runtime fallback")
				return check.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				check := material.CheckBox(a.theme, &a.embedModules, "Embed imported modules")
				return check.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				check := material.CheckBox(a.theme, &a.copyLicenses, "Copy package licenses")
				return check.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: 12}.Layout(gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Caption(a.theme, fmt.Sprintf("13-language matrix | GUI thread pinned | %d Ps", runtime.GOMAXPROCS(0)))
				label.Color = color.NRGBA{R: 110, G: 118, B: 129, A: 255}
				return label.Layout(gtx)
			}),
		)
	})
}
func smallButton(gtx layout.Context, th *material.Theme, click *widget.Clickable, label string) layout.Dimensions {
	btn := material.Button(th, click, label)
	btn.Background = color.NRGBA{R: 236, G: 242, B: 248, A: 255}
	btn.Color = color.NRGBA{R: 31, G: 35, B: 40, A: 255}
	btn.CornerRadius = 7
	btn.TextSize = unit.Sp(12)
	btn.Inset = layout.Inset{Top: 6, Bottom: 6, Left: 11, Right: 11}
	return btn.Layout(gtx)
}

const cliHelp = `Semantic Programming Language CLI - complete command reference

COMMAND ALIASES (EXACTLY EQUIVALENT)
  sp <command> [options]     ==    CodeTranspiler.exe <command> [options]
  Every command listed here can be invoked with either name.

GENERAL
  CodeTranspiler.exe
  CodeTranspiler.exe gui
  CodeTranspiler.exe help
  CodeTranspiler.exe --help
  CodeTranspiler.exe -h
  CodeTranspiler.exe version
  CodeTranspiler.exe --version
  CodeTranspiler.exe targets
  CodeTranspiler.exe languages
  CodeTranspiler.exe runtimes
  CodeTranspiler.exe routes
  CodeTranspiler.exe licenses
  CodeTranspiler.exe setpath
  CodeTranspiler.exe bundle-info [path]
  CodeTranspiler.exe bundle-verify [path]
  CodeTranspiler.exe bundle-extract <bundle> <directory>

SEMANTIC / UAST
  CodeTranspiler.exe semantic-export -source <language> input -o program.semantic.json
  CodeTranspiler.exe semantic-export -source <language> input -format sp -o program.sp
  CodeTranspiler.exe semantic-export -native -source go input.go -o program.json
  CodeTranspiler.exe semantic-export -input executable program.exe -o program.semantic.json
  CodeTranspiler.exe semantic-transpile -target <language> program.semantic.json -o output
  CodeTranspiler.exe semantic-transpile -target <language> program.se -o output
  CodeTranspiler.exe semantic-transpile -target <language> program.sp -o output
  CodeTranspiler.exe semantic-transpile -target <language> program.spz -o output
  CodeTranspiler.exe semantic-convert input.json -o output.se
  CodeTranspiler.exe semantic-convert input.se -o output.spz
  CodeTranspiler.exe semantic-merge a.se b.sp c.spz -o merged.se
  CodeTranspiler.exe semantic-format input.se --readable -o readable.se
  CodeTranspiler.exe semantic-format input.se --compact -o compact.se
  CodeTranspiler.exe semantic-validate input.se|input.sp|input.spz|input.json
  CodeTranspiler.exe semantic-info input.se|input.sp|input.spz|input.json
  CodeTranspiler.exe native-analysis -source <language> input -o analysis.json
  CodeTranspiler.exe machine-ir -input executable program.exe -o machine-ir.json
  CodeTranspiler.exe decompile -input executable program.exe -o program.semantic.json

SEMANTIC MODULES
  CodeTranspiler.exe module create a.se b.spz -o mymodule.smod
  CodeTranspiler.exe module merge a.se b.sp -o mymodule.smod
  CodeTranspiler.exe module import <source|module.se|module.spz>
  CodeTranspiler.exe module import --language go <source.go>
  CodeTranspiler.exe module import --language python six
  CodeTranspiler.exe module import --language rust itoa
  CodeTranspiler.exe module import --language r jsonlite
  CodeTranspiler.exe module import --language java org.apache.commons:commons-lang3
  CodeTranspiler.exe module path
  CodeTranspiler.exe module setpath <parent-folder>
  CodeTranspiler.exe module setpath --default
  CodeTranspiler.exe module list
  CodeTranspiler.exe module info <cache-key>
  CodeTranspiler.exe module verify <cache-key>
  CodeTranspiler.exe module remove <cache-key>
  Every module command also works as: sp module <command> ...

CAPABILITIES
  CodeTranspiler.exe capability <target> <feature>
  CodeTranspiler.exe capability-matrix [feature ...]
  CodeTranspiler.exe implementation-matrix

EMBEDDED R RUN
  CodeTranspiler.exe run input.R

RUN THROUGH TARGET LANGUAGE
  CodeTranspiler.exe run -target go input.R
  CodeTranspiler.exe run -target rust input.R
  CodeTranspiler.exe run -target cpp input.R
  CodeTranspiler.exe run -target c input.R
  CodeTranspiler.exe run -target python input.R
  CodeTranspiler.exe run -target zig input.R
  CodeTranspiler.exe run -target julia input.R
  CodeTranspiler.exe run -target nim input.R
  CodeTranspiler.exe run -target csharp input.R
  CodeTranspiler.exe run -target java input.R
  CodeTranspiler.exe run -target kotlin input.R
  CodeTranspiler.exe run -target swift input.R

TRANSPILATION
  CodeTranspiler.exe transpile -target go input.R -o output.go
  CodeTranspiler.exe transpile -target rust input.R -o output.rs
  CodeTranspiler.exe transpile -target cpp input.R -o output.cpp
  CodeTranspiler.exe transpile -target c input.R -o output.c
  CodeTranspiler.exe transpile -target python input.R -o output.py
  CodeTranspiler.exe transpile -target zig input.R -o output.zig
  CodeTranspiler.exe transpile -target julia input.R -o output.jl
  CodeTranspiler.exe transpile -target nim input.R -o output.nim
  CodeTranspiler.exe transpile -target csharp input.R -o output.cs
  CodeTranspiler.exe transpile -target java input.R -o Main.java
  CodeTranspiler.exe transpile -target kotlin input.R -o output.kt
  CodeTranspiler.exe transpile -target swift input.R -o output.swift

NATIVE COMPILE
  CodeTranspiler.exe compile input.se -o input.exe
  CodeTranspiler.exe compile input.sp -o input.exe
  CodeTranspiler.exe compile input.spz -o input.exe
  CodeTranspiler.exe compile input.json -o input.exe
  CodeTranspiler.exe compile -source go -target native-x86_64-windows input.go -o program.exe
  CodeTranspiler.exe compile -source go file1.go file2.go -o program.exe
  CodeTranspiler.exe compile -source go package-directory -o program.exe

BATCH / ANALYSIS
  CodeTranspiler.exe transpile-batch
  CodeTranspiler.exe run -source <language|auto> -target <embedded|language> input

The -o option is optional. Without it, Code Transpiler chooses the output extension.

TARGET TOOLS USED BY run -target
  Go: go
  Rust: rustc
  C++: g++ / clang++
  C: gcc / clang
  Python: python / python3 / py
  Zig: zig
  Julia: julia
  Nim: nim
  C#: csc / dotnet
  Java: javac + java
  Kotlin: kotlinc + java
  Swift: swift / swiftc

Runtime support source is compiled into CodeTranspiler.exe. Native compilers are external.
run -target materializes runtime source in its temporary work directory. Language coverage is experimental.

Open CMD starts in the CodeTranspiler.exe directory, executes CodeTranspiler.exe help
automatically, and then stays open.`
