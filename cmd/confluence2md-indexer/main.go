package main

import (
	"os"

	"github.com/gkoos/confluence2md-indexer/internal/cli"
)

func main() {
	app := cli.NewApp()
	os.Exit(app.Run(os.Args[1:]))
}
