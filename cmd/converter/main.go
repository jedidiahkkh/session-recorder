package main

import (
	"flag"
	"fmt"
	"os"
	"record-session/internal/ansi"
	"regexp"
)

type ConvertMode string

const (
	ModeJoined    ConvertMode = "joined"
	ModeSnapshots ConvertMode = "snapshots"
)

func (m *ConvertMode) String() string { return string(*m) }

func (m *ConvertMode) Set(s string) error {
	switch ConvertMode(s) {
	case ModeJoined, ModeSnapshots:
		*m = ConvertMode(s)
		return nil
	default:
		return fmt.Errorf("must be 'joined' or 'snapshots'")
	}
}

func main() {
	var mode ConvertMode
	flag.Var(&mode, "mode", "output mode: joined or snapshots")
	flag.Parse()

	if mode == "" {
		fmt.Fprintln(os.Stderr, "error: -mode is required (joined or snapshots)")
		flag.Usage()
		os.Exit(1)
	}

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: converter -mode=joined|snapshots <file.ansi>")
		os.Exit(1)
	}
	input := args[0]

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

	var convertErr error
	switch mode {
	case ModeJoined:
		convertErr = ansi.ConvertJoined(in, out)
	case ModeSnapshots:
		convertErr = ansi.ConvertSnapshots(in, out)
	}
	if convertErr != nil {
		fmt.Fprintln(os.Stderr, "error: could not convert input file: ", input, ": ", convertErr)
		os.Exit(1)
	}

	fmt.Println("successfully converted ", input, " to ", filename+".html")
}
