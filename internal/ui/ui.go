package ui

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"os"
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

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/highlight"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
	"github.com/tarekwasfy01/Code-Transpiler/internal/platform"
)

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
	return out
}()

type App struct {
	window      *app.Window
	theme       *material.Theme
	hl          *highlight.Service
	left, right *gvcode.Editor

	convertBtn, copyBtn, saveBtn, infoBtn, copyInfoBtn, closeInfoBtn, openCMDBtn, runBtn widget.Clickable
	sourceBtn, targetBtn                                                                 widget.Clickable
	sourceClicks, targetClicks                                                           []widget.Clickable
	sourceOpen, targetOpen                                                               bool
	source, target                                                                       int

	showInfo  bool
	showRun   bool
	runOutput string
	status    string
	busy      bool

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
	w.Option(app.Title("Code Transpiler"), app.Size(unit.Dp(1280), unit.Dp(760)), app.MinSize(unit.Dp(900), unit.Dp(560)))
	a := &App{
		window: w, theme: th, status: "Ready",
		convertResults: make(chan conversionResult, 4),
		runResults:     make(chan runResult, 2),
		saveResults:    make(chan saveResult, 2),
		sourceClicks:   make([]widget.Clickable, len(uiLanguages)),
		targetClicks:   make([]widget.Clickable, len(uiLanguages)),
		source:         0,
		target:         1,
	}
	a.hl = highlight.NewService(w.Invalidate)
	a.left = newCodeEditor(th, false, codeColorScheme(true, true))
	a.right = newCodeEditor(th, true, codeColorScheme(true, true))
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
				a.status = "R runtime error"
			} else {
				a.status = "R script finished"
			}
		case res := <-a.saveResults:
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
	if a.infoBtn.Clicked(gtx) {
		a.showInfo = !a.showInfo
	}
	if a.closeInfoBtn.Clicked(gtx) {
		a.showInfo = false
	}
	if a.copyInfoBtn.Clicked(gtx) {
		gtx.Execute(clipboard.WriteCmd{Type: "text/plain", Data: io.NopCloser(strings.NewReader(cliHelp))})
		a.status = "Copied CLI commands"
	}
	if a.openCMDBtn.Clicked(gtx) {
		exe, _ := os.Executable()
		if err := platform.OpenCMD(exe); err != nil {
			a.status = "Open CMD failed: " + err.Error()
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
	a.status = "Converting " + a.currentSource().Name + " → " + a.currentTarget().Name + "…"
	reader := a.left.GetReader()
	source := a.currentSource().ID
	target := a.currentTarget().ID
	lang := a.langForTarget()
	go func() {
		data, err := io.ReadAll(reader)
		code := ""
		var toks []syntax.Token
		if err == nil {
			code, err = manytomany.Transpile(source, target, string(data))
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
	if a.currentSource().ID != "r" {
		a.status = "Run currently supports R input; use Convert for other languages"
		return
	}
	if a.cancelRun != nil {
		a.cancelRun()
	}
	_, cancel := context.WithCancel(context.Background())
	a.cancelRun = cancel
	gen := a.runGeneration.Add(1)
	a.busy = true
	a.status = "Running R with embedded runtime…"
	reader := a.left.GetReader()
	go func() {
		data, err := io.ReadAll(reader)
		out := ""
		if err == nil {
			out, err = backend.Run(string(data))
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
		select {
		case a.saveResults <- saveResult{path: path, err: err}:
		default:
		}
		a.window.Invalidate()
	}()
}
func (a *App) layout(gtx layout.Context) layout.Dimensions {
	if a.busy {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(250 * time.Millisecond)})
	}
	return layout.Inset{Top: 12, Bottom: 10, Left: 12, Right: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(a.layoutHeader),
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
func (a *App) layoutHeader(gtx layout.Context) layout.Dimensions {
	runLabel := "Run R"
	if a.currentSource().ID != "r" {
		runLabel = "Run (R only)"
	}
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
			return layout.Inset{Left: 14, Right: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					btn := material.Button(a.theme, &a.convertBtn, "Convert")
					btn.Background = color.NRGBA{R: 9, G: 105, B: 218, A: 255}
					btn.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
					btn.CornerRadius = unit.Dp(8)
					btn.Inset = layout.Inset{Top: 12, Bottom: 12, Left: 20, Right: 20}
					return btn.Layout(gtx)
				})
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return a.layoutEditorPanel(gtx, "Output · "+a.currentTarget().Name, a.right)
		}),
	)
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
func (a *App) layoutRunOutput(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: color.NRGBA{R: 208, G: 215, B: 222, A: 255}, Width: 1, CornerRadius: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 8, Bottom: 8, Left: 10, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Caption(a.theme, "Embedded R runtime output")
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
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						label := material.Body2(a.theme, cliHelp)
						label.Color = color.NRGBA{R: 31, G: 35, B: 40, A: 255}
						return label.Layout(gtx)
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
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} }),
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

const cliHelp = `Code Transpiler CLI - complete command reference

GENERAL
  CodeTranspiler.exe
  CodeTranspiler.exe gui
  CodeTranspiler.exe help
  CodeTranspiler.exe version
  CodeTranspiler.exe targets
  CodeTranspiler.exe runtimes

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
