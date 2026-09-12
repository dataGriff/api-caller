// Command apic runs .http request files for humans and AI agents.
package main

import (
	"os"

	"github.com/dataGriff/api-caller/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
