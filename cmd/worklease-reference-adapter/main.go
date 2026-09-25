// Command worklease-reference-adapter is a network-free writable external adapter example.
package main

import (
	"fmt"
	"os"

	"github.com/brettinternet/worklease/internal/sampleadapter"
)

func main() {
	if err := sampleadapter.RunReference(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "reference adapter stopped after a protocol framing error")
	}
}
