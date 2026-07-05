package ts

import (
	"errors"
	"strings"
	"testing"

	"github.com/iceisfun/gojs/interp"
)

// TestUnsupportedSyntaxRejected covers that JSX and decorators — which the
// isolatedModules transpiler would preserve verbatim into JavaScript the gojs
// parser then rejects with a confusing message — are instead refused up front
// with a clear *UnsupportedSyntaxError naming the feature.
func TestUnsupportedSyntaxRejected(t *testing.T) {
	cases := []struct {
		name        string
		file        string
		src         string
		wantFeature string
	}{
		{
			"jsx element",
			"c.tsx",
			"/** @jsx h */\nfunction h(t: string){return t;}\nconsole.log(<div>hi</div>);\n",
			"JSX",
		},
		{
			"jsx self-closing",
			"c.tsx",
			"const x = <br/>;\n",
			"JSX",
		},
		{
			"jsx fragment",
			"c.tsx",
			"const x = <>hi</>;\n",
			"JSX",
		},
		{
			"class decorator",
			"c.ts",
			"function dec(_: any){}\n@dec\nclass C {}\n",
			"decorators",
		},
		{
			"method decorator",
			"c.ts",
			"function dec(_: any, __: any){}\nclass C { @dec m(){} }\n",
			"decorators",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Text path: Transpile.
			_, err := Transpile(tc.file, tc.src)
			var ue *UnsupportedSyntaxError
			if !errors.As(err, &ue) {
				t.Fatalf("Transpile error = %v, want *UnsupportedSyntaxError", err)
			}
			if !strings.Contains(ue.Feature, tc.wantFeature) {
				t.Errorf("feature = %q, want to contain %q", ue.Feature, tc.wantFeature)
			}

			// End-to-end via RunString (also the text path).
			_, rerr := RunString(interp.New(), tc.file, tc.src)
			if !errors.As(rerr, &ue) {
				t.Errorf("RunString error = %v, want *UnsupportedSyntaxError", rerr)
			}
		})
	}
}

// TestUnsupportedSyntaxViaProvider covers that a required TypeScript module using
// JSX/decorators fails with the clear error through BOTH provider paths: the
// default direct-AST frontend (which must hard-fail, not fall back to the text
// path) and the text path (DisableAST).
func TestUnsupportedSyntaxViaProvider(t *testing.T) {
	src := map[string]string{
		"jsx.tsx":  "const x = <div>hi</div>;\nmodule.exports = 1;\n",
		"deco.ts":  "function d(_: any){}\n@d\nclass C {}\nmodule.exports = 1;\n",
		"plain.ts": "export const v: number = 7;\n",
	}
	for _, disableAST := range []bool{false, true} {
		for _, entry := range []string{"./jsx.tsx", "./deco.ts"} {
			base := interp.NewMapModuleProvider(src)
			opts := []Option{}
			if disableAST {
				opts = append(opts, DisableAST())
			}
			vm := interp.New(interp.WithModuleProvider(Provider(base, opts...)))
			_, err := vm.RunString("<entry>", `require("`+entry+`")`)
			vm.Close()
			if err == nil {
				t.Errorf("disableAST=%v %s: expected error, got nil", disableAST, entry)
				continue
			}
			if !strings.Contains(err.Error(), "not supported") {
				t.Errorf("disableAST=%v %s: error = %v, want a clear 'not supported' message", disableAST, entry, err)
			}
		}
	}
}

// TestTsxWithoutJsxStillRuns guards that a .tsx file that contains no JSX is not
// falsely rejected — only actual JSX/decorator syntax is refused.
func TestTsxWithoutJsxStillRuns(t *testing.T) {
	v, err := RunString(interp.New(), "plain.tsx", "const n: number = 21;\nn * 2;\n")
	if err != nil {
		t.Fatal(err)
	}
	if v != interp.Number(42) {
		t.Fatalf("got %v, want 42", v)
	}
}
