package main

import "os"

func main() {
	os.Stdout.WriteString("stdout\n")

	if len(os.Args) > 1 && os.Args[1] == "fail" {
		os.Stderr.WriteString("stderr\n")
		os.Exit(2)
	}
}
