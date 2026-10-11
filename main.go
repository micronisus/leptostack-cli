package main

import (
	"os"

	"github.com/micronisus/leptostack-cli/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
