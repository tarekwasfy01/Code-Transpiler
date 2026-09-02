package backend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func nativeScalarCorpus() (string, string) {
	var source, want strings.Builder
	source.WriteString("package main\nimport \"fmt\"\nfunc main(){\n")
	for i := 0; i < 32; i++ {
		fmt.Fprintf(&source, `{ var active bool = true; text := "case%d"
{ text := "shadow"; fmt.Println(text) }
for active { fmt.Println(text); active = false }
if !active && text == "case%d" { fmt.Println("done") } else { fmt.Println("wrong") }
if text == "different" {fmt.Println("wrong equality")}
if text != "different" {fmt.Println("different")}
}
`, i, i)
		fmt.Fprintf(&want, "shadow\ncase%d\ndone\ndifferent\n", i)
	}
	source.WriteString("}\n")
	return source.String(), want.String()
}
func TestNativeGoExecutableRoundtrip(t *testing.T) {
	source, want := nativeScalarCorpus()
	p, err := LowerNativeGo("native.go", source)
	if err != nil {
		t.Fatal(err)
	}
	observed := ObserveSemantic(p)
	if observed.Error != "" || observed.Stdout != want {
		t.Fatalf("direct native execution mismatch: %s\n%s", observed.Error, observed.Stdout)
	}
	data, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"source"`)) {
		t.Fatal("missing source mapping")
	}
	q, err := ParseSemanticJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := q.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, again) {
		t.Fatal("native roundtrip changed document")
	}
	if !EquivalentSemanticObservations(observed, ObserveSemantic(q)) {
		t.Fatal("roundtrip execution/effects changed")
	}
	// Only validated target contracts are enabled for native source semantics.
	for _, target := range Backends() {
		_, err := EmitSemantic(target.ID, q)
		if BackendCapability("native.go.scalar", target.ID).Status != CapabilityUnsupported && err != nil {
			t.Fatalf("go: %v", err)
		}
		if BackendCapability("native.go.scalar", target.ID).Status == CapabilityUnsupported && err == nil {
			t.Fatalf("unverified native target %s accepted", target.ID)
		}
	}
	if os.Getenv("CODETRANSPILER_NATIVE_E2E") != "1" {
		t.Log("external native comparison NOT RUN; set CODETRANSPILER_NATIVE_E2E=1")
		return
	}
	generated, err := EmitSemantic("go", q)
	if err != nil {
		t.Fatal(err)
	}
	for name, code := range map[string]string{"original": source, "generated": generated} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "main.go")
			if err := os.WriteFile(file, []byte(code), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "run", file)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("%v %s", err, stderr.String())
			}
			if string(output) != want || stderr.Len() != 0 {
				t.Fatalf("native observable mismatch stdout=%q stderr=%q", output, stderr.String())
			}
		})
	}
}

func TestNativeGoExecutableRejectsUnsupported(t *testing.T) {
	cases := []string{
		`package main; func main(){x:=uint64(9007199254740993);_ = x}`,
		`package main; import "fmt"; func main(){fmt.Println(1)}`,
		`package main; import "fmt"; func main(){fmt.Println("unicode: ä")}`,
		`package main; func other(){other()};func main(){other()}`,
		`package main; func main(){if false {x:=1;_=x}}`,
		`package main; func main(){go func(){}()}`,
		`package main; func main(){x,y:=true,false;_,_=x,y}`,
	}
	for i, source := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if _, err := LowerNativeGo("unsupported.go", source); err == nil {
				t.Fatal("unsupported source silently accepted")
			}
		})
	}
}
