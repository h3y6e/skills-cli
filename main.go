package main

import (
	"os"

	"github.com/h3y6e/skolar/cmd"
)

var version = "dev"

func main() {
	if err := cmd.Execute(version); err != nil {
		os.Exit(1)
	}
}
