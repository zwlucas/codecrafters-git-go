package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type TreeNode struct {
	Mode int
	Name string
	Hash string
}

type objectType uint8

const (
	COMMIT    objectType = 0b001
	TREE      objectType = 0b010
	BLOB      objectType = 0b011
	TAG       objectType = 0100
	OFS_DELTA objectType = 0b110
	REF_DELTA objectType = 0b111
)

func init_repo(createBranch bool) {
	for _, dir := range []string{".git", ".git/objects", ".git/refs"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating directory: %s\n", err)
		}
	}

	if createBranch {
		headFileContents := []byte("ref: refs/heads/master\n")
		if err := os.WriteFile(".git/HEAD", headFileContents, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %s\n", err)
		}
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

func read_nbytes(n int, reader io.Reader) (data []byte) {
	blobData := make([]byte, n)
	io.ReadFull(reader, blobData)
	return blobData
}

func read_pack(reader io.Reader) (data []byte) {
	length, _ := strconv.ParseUint(string(read_nbytes(4, reader)), 16, 32)
	if length <= 4 {
		return nil
	}
	return read_nbytes(int(length)-4, reader)
}

func type_git(hash string) (objectType string, err error) {
	reader, err := reader(hash)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	return strings.Split(read_to_next_nbytes(reader), " ")[0], nil
}

func make_branch(ref string, hash string) (err error) {
	objectType, err := type_git(hash)
	if err != nil {
		return err
	}
	if objectType != "commit" {
		return fmt.Errorf("%s isn't a commit and so can't be made a branch", hash)
	}

	if err := os.MkdirAll(".git/refs/heads", 0755); err != nil {
		return err
	}

	if err := os.WriteFile(fmt.Sprintf(".git/refs/heads/%s", ref), []byte(hash+"\n"), 0644); err != nil {
		return err
	}

	return nil
}

func read_to_next_nbytes(reader io.Reader) (header string) {
	bytes := []byte{}
	for {
		data := read_nbytes(1, reader)
		if len(data) < 1 || data[0] == 0 {
			break
		}
		bytes = append(bytes, data[0])
	}
	return string(bytes)
}

func file_for_hash(hash string) (file string, err error) {
	if len(hash) < 2 {
		return "", fmt.Errorf("provided hash isn't long enough")
	}
	directory := hash[:2]
	filename := hash[2:]

	files, err := filepath.Glob(fmt.Sprintf(".git/objects/%s/%s*", directory, filename))
	if err != nil {
		return "", err
	}

	if len(files) < 1 {
		return "", fmt.Errorf("fatal: Not a valid object name %s", hash)
	}
	if len(files) > 1 {
		return "", fmt.Errorf("provided hash isn't unique enough")
	}

	return files[0], nil
}

func reader(hash string) (reader io.ReadCloser, err error) {
	filepath, err := file_for_hash(hash)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}

	return zlib.NewReader(file)
}

func constructBlob(path string, name string, hash string) (err error) {
	blobReader, err := reader(hash)
	if err != nil {
		return err
	}
	read_to_next_nbytes(blobReader)
	blobData, err := io.ReadAll(blobReader)
	if err != nil {
		return err
	}
	return os.WriteFile(path+name, blobData, 0644)
}

func readTree(length int, reader io.Reader) []TreeNode {
	result := []TreeNode{}
	for length > 0 {
		header := read_to_next_nbytes(reader)
		parts := strings.Split(header, " ")

		mode, _ := strconv.Atoi(parts[0])
		name := parts[1]
		hash := fmt.Sprintf("%x", read_nbytes(20, reader))
		result = append(result, TreeNode{mode, name, hash})

		length -= len(header) + 21
	}
	return result
}

func constructTree(path string, hash string) (err error) {
	treeReader, err := reader(hash)
	if err != nil {
		return err
	}
	defer treeReader.Close()

	length, err := strconv.Atoi(strings.Split(string(read_to_next_nbytes(treeReader)), " ")[1])
	if err != nil {
		return err
	}
	treeNodes := readTree(length, treeReader)

	for _, treeNode := range treeNodes {
		if treeNode.Mode == 40000 {
			if err = os.Mkdir(path+treeNode.Name, 0755); err != nil {
				return err
			}
			if err = constructTree(path+treeNode.Name+"/", treeNode.Hash); err != nil {
				return err
			}
		} else {
			if err = constructBlob(path, treeNode.Name, treeNode.Hash); err != nil {
				return err
			}
		}
	}

	return nil
}

func checkout(ref string) (err error) {
	hash, err := os.ReadFile(fmt.Sprintf(".git/refs/heads/%s", ref))
	if err != nil {
		return err
	}
	stringHash := string(hash[:len(hash)-1])

	headFileContents := []byte(fmt.Sprintf("ref: refs/heads/%s\n", ref))
	if err := os.WriteFile(".git/HEAD", headFileContents, 0644); err != nil {
		return fmt.Errorf("error writing file: %s", err)
	}

	commitReader, err := reader(stringHash)
	if err != nil {
		return err
	}
	read_to_next_nbytes(commitReader)
	read_nbytes(5, commitReader)
	treeHash := string(read_nbytes(20, commitReader))
	commitReader.Close()
	return constructTree("./", treeHash)
}

func read_byte(reader io.Reader) byte {
	return read_nbytes(1, reader)[0]
}

func readTypeAndSize(reader io.Reader) (oType objectType, size uint64) {
	// first byte is special because it contains the type
	firstByte := read_byte(reader)
	oType = objectType((firstByte & 0b01110000) >> 4)
	size = uint64(firstByte & 0b1111)

	if firstByte&0b10000000 == 0 {
		return oType, size
	}

	bytesRead := 1
	for {
		b := read_byte(reader)
		bytesRead += 1
		size = size | (uint64(b&0b1111111) << ((bytesRead-2)*7 + 4))
		if b&0b10000000 == 0 {
			break
		}
	}
	return oType, size
}

func hash_data(data []byte) (hash []byte) {
	hasher := sha1.New()
	hasher.Write(data)
	return hasher.Sum(nil)
}

func write_object(data []byte) (hash []byte, err error) {
	hash = hash_data(data)

	directory := fmt.Sprintf(".git/objects/%x", hash[:1])
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, fmt.Errorf("error creating directory: %s", err)
	}

	filepath := fmt.Sprintf("%s/%x", directory, hash[1:])
	file, err := os.Create(filepath)
	if err != nil {
		return nil, fmt.Errorf("error creating file: %s", err)
	}
	defer file.Close()

	w := zlib.NewWriter(file)
	_, err = w.Write(data)
	if err != nil {
		return nil, fmt.Errorf("error writing file: %s", err)
	}
	w.Close()

	return hash, nil
}

func write_tree(data []byte) (hash []byte, err error) {
	leadingBytes := []byte(fmt.Sprintf("tree %d%c", len(data), 0))
	return write_object(append(leadingBytes, data...))
}

func write_commit(data []byte) (hash []byte, err error) {
	leadingBytes := []byte(fmt.Sprintf("commit %d%c", len(data), 0))
	return write_object(append(leadingBytes, data...))
}

func write_blob(data []byte) (hash []byte, err error) {
	leadingBytes := []byte(fmt.Sprintf("blob %d%c", len(data), 0))
	return write_object(append(leadingBytes, data...))
}

func zlib_read(size uint64, reader io.Reader) (data []byte) {
	zlibReader, _ := zlib.NewReader(reader)
	data = read_nbytes(int(size), zlibReader)
	zlibReader.Close()
	return data
}

func readSize(reader io.Reader) (size uint64) {
	size = 0
	bytesRead := 0
	for {
		b := read_byte(reader)
		bytesRead += 1
		size = size | (uint64(b&0b1111111) << ((bytesRead - 1) * 7))
		if b&0b10000000 == 0 {
			break
		}
	}
	return size
}

func applyDelta(referenceHash string, delta []byte) (targetData []byte, err error) {
	deltaBuffer := bytes.NewBuffer(delta)
	sourceLength := readSize(deltaBuffer)
	targetLength := readSize(deltaBuffer)

	objectType, err := type_git(referenceHash)
	if err != nil {
		return nil, err
	}
	sourceReader, err := reader(referenceHash)
	if err != nil {
		return nil, err
	}
	defer sourceReader.Close()
	read_to_next_nbytes(sourceReader)
	sourceData, err := io.ReadAll(sourceReader)
	if err != nil {
		return nil, err
	}
	if len(sourceData) != int(sourceLength) {
		return nil, fmt.Errorf("source object wasn't the correct length for de deltifying")
	}

	for deltaBuffer.Len() > 0 {
		command := read_byte(deltaBuffer)
		if command&0b10000000 == 0 {
			// insert
			targetData = append(targetData, read_nbytes(int(command&0b1111111), deltaBuffer)...)
		} else {
			// copy
			offset := uint32(0)
			for i := 0; i < 4; i++ {
				if command&(0b1<<i) != 0 {
					offset |= uint32(read_byte(deltaBuffer)) << (8 * i)
				}
			}
			size := uint32(0)
			for i := 0; i < 3; i++ {
				if command&(0b10000<<i) != 0 {
					size |= uint32(read_byte(deltaBuffer)) << (8 * i)
				}
			}

			targetData = append(targetData, sourceData[offset:offset+size]...)
		}
	}

	if len(targetData) != int(targetLength) {
		return nil, fmt.Errorf("target object wasn't the correct length for de deltifying")
	}

	targetData = append([]byte(fmt.Sprintf("%s %d%c", objectType, len(targetData), 0)), targetData...)
	return targetData, nil
}

func unpack(directory string, reader io.Reader) (err error) {
	packData, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	if string(packData[:4]) != "PACK" {
		return fmt.Errorf("not a valid pack")
	}

	checksum := packData[len(packData)-20:]
	packData = packData[:len(packData)-20]
	if !bytes.Equal(checksum, hash_data(packData)) {
		return fmt.Errorf("pack data did not pass checksum")
	}

	packBuffer := bytes.NewBuffer(packData)
	read_nbytes(8, packBuffer)

	objectCount := binary.BigEndian.Uint32(read_nbytes(4, packBuffer))

	for i := uint32(0); i < objectCount; i++ {
		oType, size := readTypeAndSize(packBuffer)
		switch oType {
		case COMMIT:
			write_commit(zlib_read(size, packBuffer))
		case TREE:
			write_tree(zlib_read(size, packBuffer))
		case BLOB:
			write_blob(zlib_read(size, packBuffer))
		case TAG:
			zlib_read(size, packBuffer)
		case OFS_DELTA:
			return fmt.Errorf("offset deltas aren't currently supported")
		case REF_DELTA:
			referenceHash := read_nbytes(20, packBuffer)
			data := zlib_read(size, packBuffer)
			targetData, err := applyDelta(fmt.Sprintf("%x", referenceHash), data)
			if err != nil {
				return err
			}
			write_object(targetData)
		}

	}

	return nil
}

func clone_repo(remoteUrl string, directory string) (response string, err error) {
	if remoteUrl[len(remoteUrl)-1] == '/' {
		remoteUrl = remoteUrl[:len(remoteUrl)-1]
	}

	client := http.Client{Timeout: time.Duration(30) * time.Second}
	discoveryRequest, err := http.NewRequest("GET", fmt.Sprintf("%s/info/refs?service=git-upload-pack", remoteUrl), nil)
	if err != nil {
		return "", err
	}
	discoveryResponse, err := client.Do(discoveryRequest)
	if err != nil {
		return "", err
	}
	if discoveryResponse.StatusCode != 200 {
		return "", fmt.Errorf("discovery status: %s", discoveryResponse.Status)
	}
	defer discoveryResponse.Body.Close()

	data := read_pack(discoveryResponse.Body)
	for data != nil {
		data = read_pack(discoveryResponse.Body)
	}

	data = read_pack(discoveryResponse.Body)
	refParts := strings.Split(strings.Split(string(data), "\x00")[0], " ")
	if refParts[1] != "HEAD" {
		return "", fmt.Errorf("no HEAD ref advertized")
	}
	headHash := refParts[0]

	headRef := ""
	for data := read_pack(discoveryResponse.Body); data != nil; data = read_pack(discoveryResponse.Body) {
		refParts := strings.Split(string(data), " ")
		if refParts[0] != headHash {
			continue
		}
		headRef = refParts[1]
		if headRef[len(headRef)-1] == '\n' {
			headRef = headRef[:len(headRef)-1]
		}
	}
	if headRef == "" {
		return "", fmt.Errorf("a ref that matches HEAD could not be found")
	}
	refName := headRef[strings.LastIndex(headRef, "/")+1:]

	packBody := bytes.NewBuffer([]byte(fmt.Sprintf("0032want %s\n00000009done\n", headHash)))
	packRequest, err := http.NewRequest("POST", fmt.Sprintf("%s/git-upload-pack?service=git-upload-pack", remoteUrl), packBody)
	if err != nil {
		return "", err
	}
	packRequest.Header.Add("Content-Type", "application/x-git-upload-pack-request")
	packResponse, err := client.Do(packRequest)
	if err != nil {
		return "", err
	}
	if packResponse.StatusCode != 200 {
		return "", fmt.Errorf("pack status: %s", discoveryResponse.Status)
	}
	defer packResponse.Body.Close()

	if err = os.Mkdir(directory, 0755); err != nil {
		return "", err
	}
	os.Chdir(directory)
	init_repo(false)

	read_pack(packResponse.Body)
	if err = unpack(directory, packResponse.Body); err != nil {
		return "", err
	}
	if err = make_branch(refName, headHash); err != nil {
		return "", err
	}
	if err = checkout(refName); err != nil {
		return "", err
	}

	return fmt.Sprintf("cloned remote %s to %s\n", remoteUrl, directory), nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: mygit <command> [<args>...]\n")
		os.Exit(1)
	}

	switch command := os.Args[1]; command {
	case "init":
		init_repo(true)

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

	case "clone":
		clone, err := clone_repo(os.Args[2], os.Args[3])

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error cloning repo: %s\n", err)
			os.Exit(0)
		}

		fmt.Printf("%x\n", clone)

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(0)
	}
}
