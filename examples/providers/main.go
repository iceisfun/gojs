// Example: capability providers.
//
// Demonstrates a custom PrintProvider that captures and tags all console
// output, plus the timer provider driving setTimeout/setInterval on the event
// loop. RunString drains the loop before returning, so all timer and promise
// callbacks complete.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/iceisfun/gojs"
)

// taggedPrinter is a custom PrintProvider: instead of writing to stdout it
// prefixes every line and collects them. A host might route these to a logger,
// a buffer, or a per-tenant sink.
type taggedPrinter struct {
	mu    sync.Mutex
	lines []string
}

func (p *taggedPrinter) Print(_ context.Context, msg string) {
	p.mu.Lock()
	p.lines = append(p.lines, "[log] "+msg)
	p.mu.Unlock()
}

func (p *taggedPrinter) Warn(_ context.Context, msg string) {
	p.mu.Lock()
	p.lines = append(p.lines, "[warn] "+msg)
	p.mu.Unlock()
}

func main() {
	printer := &taggedPrinter{}
	vm := gojs.New(
		gojs.WithPrintProvider(printer),
		gojs.WithTimerProvider(gojs.NewDefaultTimerProvider()),
		gojs.WithTimeProvider(gojs.NewDefaultTimeProvider()),
	)
	defer vm.Close()

	source, err := os.ReadFile("examples/providers/app.js")
	if err != nil {
		log.Fatal(err)
	}

	// RunString runs the top level, then drains the event loop so the interval,
	// timeout, and promise callbacks all fire before returning.
	if _, err := vm.RunString("app.js", string(source)); err != nil {
		log.Fatal(err)
	}

	fmt.Println("captured output:")
	for _, line := range printer.lines {
		fmt.Println("  " + line)
	}
}
