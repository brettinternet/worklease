package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	appcli "github.com/brettinternet/worklease/internal/cli"
)

func main() {
	version := flag.String("version", "dev", "version embedded in the manual")
	date := flag.String("date", time.Now().UTC().Format(time.DateOnly), "manual date (YYYY-MM-DD)")
	flag.Parse()
	root := appcli.NewRootCommand(*version, "unknown", "unknown", os.Stdout, os.Stderr)
	if err := appcli.WriteManPage(os.Stdout, root, *date); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
