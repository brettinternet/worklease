// Command worklease-sample-adapter is a static read-only external adapter example.
package main

import (
	"fmt"
	"os"

	"github.com/brettinternet/worklease/internal/sampleadapter"
)

func main() {
	if err := sampleadapter.Run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "sample adapter stopped after a protocol framing error")
	}
}
