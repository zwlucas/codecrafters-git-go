package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

	case "hash-object":
		object := os.Args[3]
		file, err := os.ReadFile(object)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file: %s\n", err)
			os.Exit(1)
		}
		stats, err := os.Stat(object)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting file stats: %s\n", err)
			os.Exit(1)
		}
		content := string(file)
		contentAndHeader := fmt.Sprintf("blob %d\x00%s", stats.Size(), content)
		sha := sha1.Sum([]byte(contentAndHeader))
		hash := fmt.Sprintf("%x", sha)
		blobName := []rune(hash)
		blobPath := ".git/objects/"

		for i, v := range blobName {
			blobPath += string(v)
			if i == 1 {
				blobPath += "/"
			}
		}

		var buffer bytes.Buffer
		z := zlib.NewWriter(&buffer)
		z.Write([]byte(contentAndHeader))
		z.Close()

		if err := os.MkdirAll(filepath.Dir(blobPath), os.ModePerm); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating directory: %s\n", err)
			os.Exit(1)
		}

		f, err := os.Create(blobPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating file: %s\n", err)
			os.Exit(1)
		}
		defer f.Close()

		if _, err := f.Write(buffer.Bytes()); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing to file: %s\n", err)
			os.Exit(1)
		}

		fmt.Print(hash)

	case "ls-tree":
		fileNames := []string{}
		files, err := os.ReadDir(".")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unknown error %s\n", err)
			os.Exit(1)
		}

		for _, file := range files {
			if file.Name() != ".git" {
				fileNames = append(fileNames, file.Name())
			}
		}

		sort.Strings(fileNames)
		fmt.Println(strings.Join(fileNames, "\n"))

	case "write-tree":
		currentDir, _ := os.Getwd()
		h, c := calcTreeHash(currentDir)
		treeHash := hex.EncodeToString(h)
		os.Mkdir(filepath.Join(".git", "objects", treeHash[:2]), 0755)
		var compressed bytes.Buffer
		w := zlib.NewWriter(&compressed)
		w.Write(c)
		w.Close()
		os.WriteFile(filepath.Join(".git", "objects", treeHash[:2], treeHash[2:]), compressed.Bytes(), 0644)
		fmt.Println(treeHash)

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}

func calcTreeHash(dir string) ([]byte, []byte) {
	fileInfos, _ := ioutil.ReadDir(dir)

	type entry struct {
		fileName string
		b        []byte
	}

	var entries []entry
	contentSize := 0

	for _, fileInfo := range fileInfos {
		if fileInfo.Name() == ".git" {
			continue
		}

		if !fileInfo.IsDir() {
			f, _ := os.Open(filepath.Join(dir, fileInfo.Name()))
			b, _ := ioutil.ReadAll(f)
			s := fmt.Sprintf("blob %d\u0000%s", len(b), string(b))
			sha1 := sha1.New()
			io.WriteString(sha1, s)
			s = fmt.Sprintf("100644 %s\u0000", fileInfo.Name())
			b = append([]byte(s), sha1.Sum(nil)...)
			entries = append(entries, entry{fileInfo.Name(), b})
			contentSize += len(b)
		} else {
			b, _ := calcTreeHash(filepath.Join(dir, fileInfo.Name()))
			s := fmt.Sprintf("40000 %s\u0000", fileInfo.Name())
			b2 := append([]byte(s), b...)
			entries = append(entries, entry{fileInfo.Name(), b2})
			contentSize += len(b2)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].fileName < entries[j].fileName })
	s := fmt.Sprintf("tree %d\u0000", contentSize)
	b := []byte(s)

	for _, entry := range entries {
		b = append(b, entry.b...)
	}

	sha1 := sha1.New()
	io.WriteString(sha1, string(b))
	return sha1.Sum(nil), b

}
