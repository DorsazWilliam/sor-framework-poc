// Command sor is the system-of-record binary.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "usage: sor up|serve|migrate|seed|check|openapi|call|view|meta", len(modules))
	os.Exit(2)
}
