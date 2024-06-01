package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: mygit <command> [<args>...]\n")
		os.Exit(1)
	}

	switch command := os.Args[1]; command {
	case "init":
		for _, dir := range []string{".git", ".git/objects", ".git/refs"} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating directory: %s\n", err)
			}
		}

		headFileContents := []byte("ref: refs/heads/main\n")
		if err := os.WriteFile(".git/HEAD", headFileContents, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %s\n", err)
		}

		fmt.Println("Initialized git directory")

	case "cat-file":
		object := os.Args[3]
		filePath := fmt.Sprintf(".git/objects/%s/%s", object[:2], object[2:])
		file, err := os.Open(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening file: %s\n", err)
			os.Exit(1)
		}
		defer file.Close()

		r, err := zlib.NewReader(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating zlib reader: %s\n", err)
			os.Exit(1)
		}
		defer r.Close()

		w, err := io.ReadAll(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading zlib data: %s\n", err)
			os.Exit(1)
		}

		parts := bytes.Split(w, []byte("\x00"))
		if len(parts) < 2 {
			fmt.Fprintf(os.Stderr, "Invalid zlib data\n")
			os.Exit(1)
		}

		fmt.Print(string(parts[1]))

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}
