// Package main owns the esheep process boundary.
package main

import (
	"os"

	"github.com/jmcampanini/esheep/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
