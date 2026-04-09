package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"record-session/internal/ansi"
	"record-session/internal/recorder"
)

const sessionDir = ".record-sessions"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: record-session <command> [args...]\n")
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error: could not create session directory: %v\n", err)
		os.Exit(1)
	}

	timestamp := time.Now().Format("20060102-150405")
	base := filepath.Join(sessionDir, timestamp+"-session")
	ansiPath := base + ".ansi"
	htmlPath := base + ".html"

	if err := recorder.Record(ansiPath, command, args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	ansiFile, err := os.Open(ansiPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not open session file: %v\n", err)
		os.Exit(1)
	}
	defer ansiFile.Close()

	htmlFile, err := os.Create(htmlPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not create HTML file: %v\n", err)
		os.Exit(1)
	}
	defer htmlFile.Close()

	if err := ansi.ConvertSnapshots(ansiFile, htmlFile); err != nil {
		fmt.Fprintf(os.Stderr, "error: could not convert session to HTML: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("session saved: %s\n", htmlPath)
}
