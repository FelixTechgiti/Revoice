package speaker

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

// The jack-aware output chain (#231) lives in pcm_speaker.go and
// outputchain.go, both `//go:build server`, so the host suite cannot run a
// line of it — a `go test ./...` here reports success without having compiled
// it at all. What can still be pinned is the WIRING, which is where the whole
// feature is: resolution against the plug position, and the second call site
// that makes a plug change reach the chain.
//
// Comments are stripped before matching, or these assertions pass against the
// paragraph explaining the rule instead of the code obeying it.

func funcBody(t *testing.T, file, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0) // no ParseComments: stripped
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name || fn.Body == nil {
			continue
		}
		var sb strings.Builder
		if err := printer.Fprint(&sb, fset, fn.Body); err != nil {
			t.Fatalf("print %s: %v", name, err)
		}
		return sb.String()
	}
	t.Fatalf("%s: no func %s — has it been renamed?", file, name)
	return ""
}

// Without this call a plug change moves the codec and leaves the chain on
// whatever the last config push resolved to — so the bypass would work only
// for someone who saved a setting while the cable was already in, and would
// never come back when it was pulled out.
func TestSetJackRoutingReResolvesTheOutputChain(t *testing.T) {
	body := funcBody(t, "pcm_speaker.go", "SetJackRouting")
	if !strings.Contains(body, "applyOutputChainParams()") {
		t.Error("SetJackRouting no longer re-resolves the output chain — a " +
			"plug change will move the codec and leave the bass guard where " +
			"the last config push put it (#231)")
	}
}

// The single-resolution rule. Two paths reach the chain and both must go
// through ForJack; a SetParams anywhere else is the unresolved set reaching a
// filter, which Params.Equal cannot catch because it deliberately ignores the
// policy field.
func TestTheChainIsOnlyEverHandedResolvedParameters(t *testing.T) {
	resolve := funcBody(t, "outputchain.go", "applyOutputChainParams")
	if !strings.Contains(resolve, "ForJack(inserted)") {
		t.Error("applyOutputChainParams does not call ForJack — the chain is " +
			"being handed the pushed parameters unresolved")
	}
	if !strings.Contains(resolve, "jackKnown && p.jackInserted") {
		t.Error("the plug position is no longer read as jackKnown && " +
			"jackInserted — an UNKNOWN position must resolve to 'not " +
			"inserted', which keeps the guard on")
	}

	push := funcBody(t, "outputchain.go", "SetOutputChain")
	if strings.Contains(push, "SetParams(") || strings.Contains(push, "NewChain(") {
		t.Error("SetOutputChain touches the chain directly — it must store " +
			"the pushed set and let applyOutputChainParams resolve it, or a " +
			"push with a plug in would install the unresolved parameters")
	}
	if !strings.Contains(push, "applyOutputChainParams()") {
		t.Error("SetOutputChain no longer applies anything")
	}
}
