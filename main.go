// Command skolar manages local agent skills from git repositories.
package main

import (
	"os"

	"github.com/h3y6e/skolar/cmd"
)

var version = "dev"

func main() {
	os.Exit(cmd.Execute(version))
}
