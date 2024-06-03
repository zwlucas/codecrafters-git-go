package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

func init_repo() {
	for _, dir := range []string{".git", ".git/objects", ".git/refs"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating directory: %s\n", err)
		}
	}

	headFileContents := []byte("ref: refs/heads/master\n")
	if err := os.WriteFile(".git/HEAD", headFileContents, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %s\n", err)
	}

	fmt.Println("Initialized git directory")
}

func read_obj(blob_sha string) []byte {
	path := fmt.Sprintf(".git/objects/%s/%s", blob_sha[:2], blob_sha[2:])

	file, err := os.Open(path)
	if err != nil {
		fmt.Printf("Failed to open blob file: %s\n", err)
	}

	reader, err := zlib.NewReader(file)
	if err != nil {
		fmt.Printf("Failed to instantiate zlib reader: %s\n", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			fmt.Printf("Failed to close zlib reader: %s\n", err)
		}
	}()

	var buffer bytes.Buffer

	_, err = io.Copy(&buffer, reader)
	if err != nil {
		fmt.Printf("Failed to write to stdout: %s\n", err)
	}

	_, err = buffer.ReadBytes(byte(' '))
	if err != nil {
		fmt.Printf("Failed to read from buffer: %s\n", err)
	}

	size_byte, err := buffer.ReadBytes(byte(0))
	if err != nil {
		fmt.Printf("Failed to read from buffer: %s\n", err)
	}

	size, err := strconv.Atoi(string(size_byte[:len(size_byte)-1]))
	if err != nil {
		fmt.Printf("Failed to convert number of bytes into integer: %s\n", err)
	}

	buffer.Truncate(size)
	return buffer.Bytes()
}

func create_obj(content []byte) []byte {
	hash_writer := sha1.New()
	var blob_content_buffer bytes.Buffer
	zlib_writer := zlib.NewWriter(&blob_content_buffer)
	writer := io.MultiWriter(hash_writer, zlib_writer)
	writer.Write(content)

	sha := hash_writer.Sum(nil)
	sha_string := fmt.Sprintf("%x", sha)

	zlib_writer.Close()

	blob_dir := fmt.Sprintf(".git/objects/%s", sha_string[:2])

	err := os.MkdirAll(blob_dir, 0755)
	if err != nil {
		fmt.Printf("Failed to create directory for object: %s\n", err)
	}

	blob_path := fmt.Sprintf("%s/%s", blob_dir, sha_string[2:])

	err = os.WriteFile(blob_path, blob_content_buffer.Bytes(), 0644)
	if err != nil {
		fmt.Printf("Failed to write blob to file: %s\n", err)
	}

	return sha
}

func hash_file(path string) []byte {
	f, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("Failed to read from given file: %s\n", err)
	}

	content := []byte(fmt.Sprintf("blob %d\x00", len(f)))
	content = append(content, f...)
	return create_obj(content)
}

func read_tree(hash string) {
	file, err := os.Open(fmt.Sprintf(".git/objects/%s/%s", hash[:2], hash[2:]))
	if err != nil {
		fmt.Printf("Failed to open tree object file: %s\n", err)
	}

	reader, err := zlib.NewReader(file)

	if err != nil {
		fmt.Printf("Failed to instantiate zlib reader: %s\n", err)
	}

	defer func() {
		if err := reader.Close(); err != nil {
			fmt.Printf("Failed to close zlib reader: %s\n", err)
		}
	}()

	var buffer bytes.Buffer

	_, err = io.Copy(&buffer, reader)
	if err != nil {
		fmt.Printf("Failed to write to stdout: %s\n", err)
	}

	_, err = buffer.ReadBytes(byte(' '))
	if err != nil {
		fmt.Printf("Failed to read from buffer: %s\n", err)
	}

	size_byte, _ := buffer.ReadBytes(byte(0))

	_, err = strconv.Atoi(string(size_byte[:len(size_byte)-1]))
	if err != nil {
		fmt.Printf("Failed to convert tree object size to integer: %s\n", err)
	}

	sha_buffer := make([]byte, 20)

	for {
		_, err = buffer.ReadBytes(byte(' '))
		if err != nil {
			fmt.Printf("Failed to read from buffer first: %s\n", err)
		}

		name, err := buffer.ReadBytes(byte(0))
		if err != nil {
			fmt.Printf("Failed to read from buffer second: %s\n", err)
		}

		fmt.Println(string(name[:len(name)-1]))

		_, err = io.ReadFull(&buffer, sha_buffer)
		if err != nil {
			fmt.Printf("Failed to read 20 bytes from buffer: %s\n", err)
		}

		if buffer.Len() == 0 {
			break
		}
	}
}

func hash_tree(dir string) []byte {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Printf("Failed to list directory: %s\n", err)
	}

	var entries_buffer bytes.Buffer

	for _, entry := range entries {
		name := entry.Name()
		path := fmt.Sprintf("%s/%s", dir, name)

		if name == ".git" {
			continue
		}

		var sha []byte
		var mode string

		if entry.IsDir() {
			mode = "40000"
			sha = hash_tree(path)
		} else {
			mode = "100644"
			sha = hash_file(path)
		}

		_, err = entries_buffer.Write([]byte(fmt.Sprintf("%s %s\x00", mode, name)))
		if err != nil {
			fmt.Printf("Failed to write to byte buffer: %s\n", err)
		}

		_, err = entries_buffer.Write(sha)
		if err != nil {
			fmt.Printf("Failed to write to byte buffer: %s\n", err)
		}
	}

	content := []byte(fmt.Sprintf("tree %d\x00", entries_buffer.Len()))
	content = append(content, entries_buffer.Bytes()...)
	return create_obj(content)
}

func commit_tree(tree_sha, parent_sha, message string) []byte {
	author := "Lucas Faria"
	authorEmail := "jet2tlf@gmail.com"
	currentUnixTime := time.Now().Unix()
	timezone, _ := time.Now().Local().Zone()

	commit_content := []byte(fmt.Sprintf(
		"tree %s\nparent %s\nauthor %s <%s> %s %s\ncommitter %s <%s> %s %s\n\n%s\n",
		tree_sha,
		parent_sha,
		author,
		authorEmail,
		fmt.Sprint(currentUnixTime),
		timezone,
		author,
		authorEmail,
		fmt.Sprint(currentUnixTime),
		timezone,
		message),
	)

	content := []byte(fmt.Sprintf("commit %d\x00", len(commit_content)))
	content = append(content, commit_content...)
	return create_obj(content)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: mygit <command> [<args>...]\n")
		os.Exit(1)
	}

	switch command := os.Args[1]; command {
	case "init":
		init_repo()

	case "cat-file":
		content := read_obj(os.Args[3])
		fmt.Print(string(content))

	case "hash-object":
		sha := hash_file(os.Args[3])
		fmt.Printf("%x\n", sha)

	case "ls-tree":
		read_tree(os.Args[3])

	case "write-tree":
		sha := hash_tree(".")
		fmt.Printf("%x\n", sha)

	case "commit-tree":
		sha := commit_tree(os.Args[2], os.Args[4], os.Args[6])
		fmt.Printf("%x\n", sha)

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}
