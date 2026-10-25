// Package main owns the esheep process boundary.
package main

import (
	"os"

	"github.com/jmcampanini/esheep/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
