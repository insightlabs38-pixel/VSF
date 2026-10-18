// Command vsfctl is the B00 Go stub of the VSF CLI and control-plane entry point.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/insightlabs38-pixel/vsf/backend/canon"
	"github.com/insightlabs38-pixel/vsf/backend/schema"
)

const usage = "usage: vsfctl <verify [FILE] | digest [FILE] | version>"

func readInput(args []string) ([]byte, error) {
	if len(args) > 0 && args[0] != "-" {
		return os.ReadFile(args[0])
	}
	return io.ReadAll(io.LimitReader(os.Stdin, canon.MaxInputBytes+1))
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 64
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "{\"schema\":\"%s/%s\"}\n", schema.SchemaName, schema.CurrentVersion)
		return 0
	case "verify", "digest":
		input, err := readInput(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "io: %v\n", err)
			return 66
		}
		_, canonical, err := schema.VerifyBytes(input)
		if err != nil {
			fmt.Fprintf(stderr, "error: %s\n", schema.ErrorCode(err))
			return 2
		}
		if args[0] == "digest" {
			fmt.Fprintln(stdout, canon.SHA256Hex(canonical))
			return 0
		}
		if _, err := stdout.Write(canonical); err != nil {
			return 74
		}
		return 0
	}
	fmt.Fprintln(stderr, usage)
	return 64
}
