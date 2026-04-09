package main

import (
	"fmt"
	"os"
	"record-session/internal/ansi"
	"regexp"
)

func main() {
	input := os.Args[1]

	in, err := os.Open(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: could not open input file: ", input, ": ", err)
		os.Exit(1)
	}
	defer in.Close()

	filenamePattern := regexp.MustCompile(`(.+)\.ansi`)
	matches := filenamePattern.FindStringSubmatch(input)
	if len(matches) < 2 {
		fmt.Fprintln(os.Stderr, "error: could not extract filename from input: ", input)
		os.Exit(1)
	}
	filename := matches[1]

	out, err := os.Create(filename + ".html")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: could not create output file: ", filename+".html", ": ", err)
		os.Exit(1)
	}
	defer out.Close()

	if err := ansi.Convert(in, out); err != nil {
		fmt.Fprintln(os.Stderr, "error: could not convert input file: ", input, ": ", err)
		os.Exit(1)
	}

	fmt.Println("successfully converted ", input, " to ", filename+".html")
}
