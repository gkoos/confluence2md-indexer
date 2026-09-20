package main

import (
	"os"

	"github.com/gkoos/confluence2md-indexer/internal/cli"
)

// version identifies the build. Release builds stamp it through
// -ldflags "-X main.version=<tag>", so a local build reports "dev".
var version = "dev"

func main() {
	app := cli.NewApp()
	app.Version = version
	os.Exit(app.Run(os.Args[1:]))
}
