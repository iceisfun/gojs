package process

import (
	"bytes"
	"strings"
	"testing"

	"github.com/iceisfun/gojs"
)

func TestProcess(t *testing.T) {
	var out bytes.Buffer
	exitCode := -1

	vm := gojs.New()
	defer vm.Close()
	if err := Install(vm,
		WithArgs("gojs", "app.js", "--flag"),
		WithEnv(map[string]string{"FOO": "bar"}),
		WithStdout(&out),
		WithExit(func(c int) { exitCode = c }),
	); err != nil {
		t.Fatal(err)
	}

	_, err := vm.RunString("t.js", `
		process.stdout.write("argv=" + process.argv.join(",") + "\n");
		process.stdout.write("env.FOO=" + process.env.FOO + "\n");
		process.stdout.write("pid=" + (typeof process.pid) + "\n");
		process.nextTick(() => process.stdout.write("tick\n"));
		process.exit(3);
	`)
	if err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, want := range []string{"argv=gojs,app.js,--flag", "env.FOO=bar", "pid=number", "tick"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
	if exitCode != 3 {
		t.Errorf("exit code = %d, want 3", exitCode)
	}
}
