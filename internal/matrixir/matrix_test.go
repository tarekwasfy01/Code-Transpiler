package matrixir

import (
	"strings"
	"testing"
)

func TestMaskedDeficitMatrix(t *testing.T) {
	support := NewMatrix(len(Languages), len(Features))
	known := NewMatrix(len(Languages), len(Features))
	goIndex, _ := LanguageIndex("go")
	arithmetic := MustFeatureIndex("arithmetic")
	comparison := MustFeatureIndex("comparison")
	known.Set(goIndex, arithmetic, 1)
	support.Set(goIndex, arithmetic, 1)
	known.Set(goIndex, comparison, 1)
	model, err := NewCapabilityModel(support, known)
	if err != nil {
		t.Fatal(err)
	}
	requirements := NewMatrix(1, len(Features))
	requirements.Set(0, arithmetic, 1)
	requirements.Set(0, comparison, 1)
	missing, unknown, err := model.DeficitMatrices(requirements)
	if err != nil {
		t.Fatal(err)
	}
	if got := missing.At(0, goIndex); got != 1 {
		t.Fatalf("missing=%v, want 1", got)
	}
	if got := unknown.At(0, goIndex); got != 0 {
		t.Fatalf("unknown=%v, want 0", got)
	}
}

func TestGraphProjectionAndRelations(t *testing.T) {
	g, err := NewGraph("python")
	if err != nil {
		t.Fatal(err)
	}
	program := g.AddNode(Basis(SemanticDimensions, SemProgram), "", "", 0)
	assignVector := Basis(SemanticDimensions, SemAssign)
	assignVector[SemBinding] = 1
	assign := g.AddNode(assignVector, "x = 2 + 3", "x", 0)
	exprVector := Basis(SemanticDimensions, SemBinary)
	exprVector[SemArithmetic] = 1
	expr := g.AddNode(exprVector, "2 + 3", "", 4)
	if err := g.Connect(Syntax, program, assign); err != nil {
		t.Fatal(err)
	}
	if err := g.Connect(Syntax, assign, expr); err != nil {
		t.Fatal(err)
	}
	req := g.Requirements()
	if req[MustFeatureIndex("binding")] != 1 || req[MustFeatureIndex("arithmetic")] != 1 {
		t.Fatalf("unexpected requirements: %v", req)
	}
	closure, err := g.Edges[Syntax].BooleanClosure()
	if err != nil {
		t.Fatal(err)
	}
	if closure.At(program, expr) != 1 {
		t.Fatal("syntax closure did not connect program to expression")
	}
}

func TestLexicalGraphKeepsCommentMarkersInsideStrings(t *testing.T) {
	graph, tokens, err := BuildLexicalGraph("python", "x = \"https://a#b\"\ny = (2 +\n3)\n")
	if err != nil {
		t.Fatal(err)
	}
	comments, stringComments, continuationNewlines := 0, 0, 0
	for _, token := range tokens {
		if token.Class == TokenComment {
			comments++
		}
		if token.Semantic[SemStringComment] != 0 {
			stringComments++
		}
		if token.Class == TokenNewline && token.Semantic[SemMultiline] != 0 {
			continuationNewlines++
		}
	}
	if comments != 0 {
		t.Fatalf("comment tokens=%d, want 0", comments)
	}
	if stringComments != 1 {
		t.Fatalf("string-comment vectors=%d, want 1", stringComments)
	}
	if continuationNewlines != 1 {
		t.Fatalf("continuation newlines=%d, want 1", continuationNewlines)
	}
	if graph.RelationCounts()[Control] != float64(len(tokens)-1) {
		t.Fatal("control matrix is not a token chain")
	}
}

func TestCBlockCommentsAreNotDivisionTokens(t *testing.T) {
	tokens := Tokenize("c", "/* header */\nint x = 4 / 2; /* tail */\n")
	comments, divisions := 0, 0
	for _, token := range tokens {
		if token.Class == TokenComment {
			comments++
		}
		if token.Class == TokenOperator && token.Text == "/" {
			divisions++
		}
	}
	if comments != 2 || divisions != 1 {
		t.Fatalf("comments=%d divisions=%d, want 2 and 1", comments, divisions)
	}
}

func TestLexicalRuleMatrixAddsStatementSemantics(t *testing.T) {
	graph, _, err := BuildLexicalGraph("python", "x = 1\nx = 2\nprint(x)\nreturn x\n")
	if err != nil {
		t.Fatal(err)
	}
	required := graph.SemanticRequirements()
	for _, semantic := range []int{SemAssign, SemBinding, SemReassignment, SemPrint, SemReturn, SemIO, SemEffect} {
		if required[semantic] == 0 {
			t.Fatalf("semantic dimension %d is absent", semantic)
		}
	}
	counts := graph.RelationCounts()
	if counts[Binding] != 2 || counts[Data] != 2 || counts[Effect] != 1 {
		t.Fatalf("relation counts=%v, want binding=2 data=2 effect=1", counts)
	}
}

func TestLexicalContextAddsCallNamedArgumentAndShortCircuit(t *testing.T) {
	graph, _, err := BuildLexicalGraph("python", "flag = true\nprint(f(value = 1) and flag)\n")
	if err != nil {
		t.Fatal(err)
	}
	required := graph.SemanticRequirements()
	for _, semantic := range []int{SemBoolean, SemCall, SemNamedArgument, SemShortCircuit} {
		if required[semantic] == 0 {
			t.Fatalf("context semantic dimension %d is absent", semantic)
		}
	}
}

func TestCanonicalMatrixParserPreservesStringsAndNormalizesStructure(t *testing.T) {
	program, err := Canonicalize("python", "x = 1\nif x < 2:\n    x = 2\nelse:\n    x = 9\nprint(\"true # false\")\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "x <- 1\nif (x < 2) {\nx <- 2\n}\nelse {\nx <- 9\n}\nprint(\"true # false\")\n"
	if program.R != want {
		t.Fatalf("canonical Python:\n%s\nwant:\n%s", program.R, want)
	}
	if program.Grammar[GrammarIndent] != 1 || program.Grammar[GrammarOneBasedIndex] != 0 {
		t.Fatalf("unexpected Python grammar vector: %v", program.Grammar)
	}
}

func TestCanonicalPythonIterableForUsesForEachInsteadOfNumericRange(t *testing.T) {
	program, err := Canonicalize("python", "for item in values:\n    print(item)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(program.R, "for (item in values) {") {
		t.Fatalf("iterable Python for was not preserved as ForEach: %s", program.R)
	}
	if strings.Contains(program.R, "__matrix_range_") {
		t.Fatalf("iterable Python for incorrectly created numeric range facts: %s", program.R)
	}
}

func TestCanonicalPythonSimpleLoopPatternUsesBindingPatternContract(t *testing.T) {
	program, err := Canonicalize("python", "for key, value in items:\n    print(value)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(program.R, "for ((key,value) in items) {") {
		t.Fatalf("loop binding pattern not preserved: %s", program.R)
	}
	if _, err := Canonicalize("python", "for key, *values in items:\n    print(key)\n"); err == nil {
		t.Fatal("starred loop pattern was accepted without a cardinality contract")
	}
}

func TestCanonicalPythonUnitStepRangeUsesExistingAscendingRangeContract(t *testing.T) {
	program, err := Canonicalize("python", "for item in range(2, 5, 1):\n    print(item)\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"_start <- 2", "_end <- 4", "for (item in "} {
		if !strings.Contains(program.R, fragment) {
			t.Fatalf("unit-step Python range missing %q:\n%s", fragment, program.R)
		}
	}
}

func TestCanonicalPythonLiteralStepRangeUsesExistingSequencePrimitive(t *testing.T) {
	program, err := Canonicalize("python", "for item in range(5, 0, -2):\n    print(item)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(program.R, "for (item in seq(5, (0) + 1, by = -2)) {") {
		t.Fatalf("signed Python range did not use the sequence primitive:\n%s", program.R)
	}
}

func TestCanonicalPythonSemicolonsAndPassUseStatementMatrix(t *testing.T) {
	program, err := Canonicalize("python", "x = 1; y = 2\nif x < y:\n    pass\nprint(y)\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"x <- 1", "y <- 2", "print(y)"} {
		if !strings.Contains(program.R, fragment) {
			t.Fatalf("Python statement matrix missing %q:\n%s", fragment, program.R)
		}
	}
	if !strings.Contains(program.R, "if (x < y) {") || strings.Contains(program.R, "pass") {
		t.Fatalf("Python pass was not treated as no-op:\n%s", program.R)
	}
}

func TestCanonicalPythonElifUsesNestedElseIfContract(t *testing.T) {
	program, err := Canonicalize("python", "if x < 0:\n    print(x)\nelif x < 2:\n    print(x)\nelse:\n    print(x)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(program.R, "else if (x < 2) {") {
		t.Fatalf("Python elif did not use nested else-if contract:\n%s", program.R)
	}
}

func TestCanonicalPythonSimpleLambdaUsesClosureContract(t *testing.T) {
	program, err := Canonicalize("python", "identity = lambda value: value\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(program.R, "identity <- function(value) { return(value) }") {
		t.Fatalf("Python lambda did not use closure contract:\n%s", program.R)
	}
}

func TestCanonicalMatrixParserUsesGrammarForDivisionRangeAndIndex(t *testing.T) {
	program, err := Canonicalize("zig", "const x = [_]i64{10, 20};\nfor (1..5) |i| {\nstd.debug.print(\"{}\\n\", .{x[1]});\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"x <- c(10, 20)", "_start <- 1", "_end <- 4", "for (i in ", "print(x[2])"} {
		if !strings.Contains(program.R, fragment) {
			t.Fatalf("canonical Zig missing %q:\n%s", fragment, program.R)
		}
	}
	goProgram, err := Canonicalize("go", "package main\nfunc main() {\nfmt.Println(7 / 2)\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(goProgram.R, "print(7 %/% 2)") {
		t.Fatalf("integer slash grammar not applied:\n%s", goProgram.R)
	}
}

func TestTokenStructureRejectsBrokenSyntaxAndPreservesLiteralDelimiters(t *testing.T) {
	for _, code := range []string{"print((2 + 3)", "x = [1, 2)", "print(\"unfinished)"} {
		if _, err := Canonicalize("python", code); err == nil {
			t.Fatalf("accepted broken syntax: %s", code)
		}
	}
	text := "x = [10, 20]; print(x[0] + x[1]); print(\"True and [0] {1,2} (double)\")"
	p, err := Canonicalize("python", text)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"x <- c(10, 20)", "print(x[1] + x[2])", "print(\"True and [0] {1,2} (double)\")"} {
		if !strings.Contains(p.R, want) {
			t.Fatalf("missing %s in %s", want, p.R)
		}
	}
	tokens := Tokenize("python", text)
	structure, err := AnalyzeTokenStructure(tokens)
	if err != nil {
		t.Fatal(err)
	}
	for i, token := range tokens {
		if token.Class == TokenString && structure.Code[i] != 0 {
			t.Fatal("literal entered code projection")
		}
	}
}

func TestStatementMatrixKeepsNestedBlocksAndZeroReturn(t *testing.T) {
	p, err := Canonicalize("go", "func zero(x int) int { return 0 }; func main() { x := 2; if x < 3 { x = 5 } else { x = 9 }; fmt.Println(zero(x)) }")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"return(0)", "x <- 2", "if (x < 3) {", "x <- 5", "else {", "x <- 9", "print(zero(x))"} {
		if !strings.Contains(p.R, want) {
			t.Fatalf("missing %q in %s", want, p.R)
		}
	}
}
