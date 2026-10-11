package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type archiveLink struct {
	name   string
	target string
	hard   bool
}

type archiveSink struct {
	root    string
	limit   int64
	written int64
	entries map[string]bool
	links   []archiveLink
}

func archivePath(root, name string) (string, error) {
	if strings.ContainsAny(name, "\\:\x00\r\n") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("archive path %q must be a relative portable path", name)
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("archive path %q escapes extraction directory %q", name, root)
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." {
			continue
		}
		if strings.TrimRight(segment, " .") != segment {
			return "", fmt.Errorf("archive path %q contains a trailing dot or space", name)
		}
		base := strings.ToUpper(strings.SplitN(segment, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9') {
			return "", fmt.Errorf("archive path %q contains reserved device name %q", name, segment)
		}
	}
	return filepath.Join(root, filepath.FromSlash(clean)), nil
}

func validateLink(root, name, target string) error {
	if target == "" || strings.ContainsAny(target, "\\:\x00\r\n") || strings.HasPrefix(target, "/") {
		return fmt.Errorf("symlink %q has invalid target %q: expected a relative target inside %q", name, target, root)
	}
	_, err := archivePath(root, path.Join(path.Dir(filepath.ToSlash(name)), target))
	if err != nil {
		return fmt.Errorf("symlink %q -> %q: %w", name, target, err)
	}
	return nil
}

func resolveContainedPath(root, relative string) error {
	pending := strings.Split(filepath.ToSlash(relative), "/")
	current := make([]string, 0)
	links := 0
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(current) == 0 {
				return fmt.Errorf("path %q resolves outside installation root %q", relative, root)
			}
			current = current[:len(current)-1]
			continue
		}
		candidate := filepath.Join(append([]string{root}, append(current, part)...)...)
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			current = append(current, part)
			continue
		}
		if err != nil {
			return fmt.Errorf("resolve contained path %q at %q: %w", relative, candidate, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			current = append(current, part)
			continue
		}
		links++
		if links > 255 {
			return fmt.Errorf("path %q contains a symlink cycle or more than 255 symlinks", relative)
		}
		target, err := os.Readlink(candidate)
		if err != nil {
			return err
		}
		name := path.Join(append(current, part)...)
		if err := validateLink(root, name, target); err != nil {
			return err
		}
		pending = append(strings.Split(target, "/"), pending...)
	}
	return nil
}

func safeParents(root, destination string) error {
	relative, err := filepath.Rel(root, filepath.Dir(destination))
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0755); err != nil {
				return fmt.Errorf("create archive directory %q: %w", current, err)
			}
		} else if err != nil {
			return fmt.Errorf("inspect archive parent %q: %w", current, err)
		} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive parent %q is not a real directory", current)
		}
	}
	return nil
}

func (sink *archiveSink) add(name string, mode os.FileMode, size int64, content io.Reader, target string, hard bool) error {
	destination, err := archivePath(sink.root, name)
	if err != nil {
		return err
	}
	if destination == sink.root {
		if mode.IsDir() {
			return nil
		}
		return fmt.Errorf("archive entry %q would replace extraction root", name)
	}
	key := strings.ToLower(filepath.Clean(destination))
	if directory, exists := sink.entries[key]; exists {
		if mode.IsDir() && directory {
			return nil
		}
		return fmt.Errorf("duplicate archive entry %q", name)
	}
	if len(sink.entries) >= 200000 {
		return fmt.Errorf("archive exceeds maximum of 200000 entries")
	}
	sink.entries[key] = mode.IsDir()
	if size < 0 || size > sink.limit-sink.written {
		return fmt.Errorf("archive entry %q of %d bytes exceeds remaining extraction allowance %d", name, size, sink.limit-sink.written)
	}
	sink.written += size
	if err := safeParents(sink.root, destination); err != nil {
		return err
	}
	if mode.IsDir() {
		if err := os.MkdirAll(destination, 0755); err != nil {
			return fmt.Errorf("create archive directory %q: %w", destination, err)
		}
		return nil
	}
	if mode&os.ModeSymlink != 0 || hard {
		if hard {
			if _, err := archivePath(sink.root, target); err != nil {
				return err
			}
		} else if err := validateLink(sink.root, name, target); err != nil {
			return err
		}
		sink.links = append(sink.links, archiveLink{name: name, target: target, hard: hard})
		return nil
	}
	if !mode.IsRegular() {
		return fmt.Errorf("archive entry %q has unsupported mode %s", name, mode)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm()|0400)
	if err != nil {
		return fmt.Errorf("create extracted file %q: %w", destination, err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(content, size+1))
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("extract file %q: %w", destination, copyErr)
	}
	if written != size {
		return fmt.Errorf("extract file %q: received %d bytes, expected %d", destination, written, size)
	}
	if closeErr != nil {
		return fmt.Errorf("close extracted file %q: %w", destination, closeErr)
	}
	return nil
}

func (sink *archiveSink) finish() error {
	for _, link := range sink.links {
		destination, err := archivePath(sink.root, link.name)
		if err != nil {
			return err
		}
		if err := safeParents(sink.root, destination); err != nil {
			return err
		}
		if link.hard {
			target, err := archivePath(sink.root, link.target)
			if err != nil {
				return err
			}
			if err := safeParents(sink.root, target); err != nil {
				return err
			}
			info, err := os.Lstat(target)
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("hard link %q requires existing regular target %q: %v", link.name, link.target, err)
			}
			if err := os.Link(target, destination); err != nil {
				return fmt.Errorf("extract hard link %q -> %q: %w", link.name, link.target, err)
			}
		} else if err := os.Symlink(link.target, destination); err != nil {
			return fmt.Errorf("extract symlink %q -> %q: %w", link.name, link.target, err)
		}
	}
	for _, link := range sink.links {
		if err := resolveContainedPath(sink.root, link.name); err != nil {
			return err
		}
	}
	return nil
}

func extractArchive(b Config, artifact, archiveName, destination string) error {
	if err := os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	sink := &archiveSink{root: destination, limit: extractionLimit(b), entries: make(map[string]bool)}
	lower := strings.ToLower(archiveName)
	var err error
	switch {
	case strings.HasSuffix(lower, ".zip"):
		err = extractZip(artifact, sink)
	case strings.HasSuffix(lower, ".tar"), strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"), strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"):
		err = extractTarFile(artifact, lower, sink)
	case strings.HasSuffix(lower, ".tar.xz"), strings.HasSuffix(lower, ".txz"):
		err = extractXZ(b, artifact, destination, sink)
	case strings.HasSuffix(lower, ".7z"), strings.HasSuffix(lower, ".7z.exe"):
		err = extractSevenZip(b, artifact, sink)
	default:
		err = fmt.Errorf("archive %q has unsupported format; supported: zip, tar, tar.gz, tar.bz2, tar.xz, 7z", archiveName)
	}
	if err != nil {
		return fmt.Errorf("extract %q into %q: %w", archiveName, destination, err)
	}
	return sink.finish()
}

func extractZip(filename string, sink *archiveSink) error {
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, entry := range reader.File {
		if entry.UncompressedSize64 > uint64(sink.limit) {
			return fmt.Errorf("zip entry %q exceeds extraction limit %d", entry.Name, sink.limit)
		}
		file, err := entry.Open()
		if err != nil {
			return err
		}
		target := ""
		if entry.Mode()&os.ModeSymlink != 0 {
			contents, readErr := io.ReadAll(io.LimitReader(file, 4097))
			if readErr != nil || len(contents) > 4096 {
				file.Close()
				return fmt.Errorf("zip symlink %q exceeds 4096 bytes or cannot be read: %v", entry.Name, readErr)
			}
			target = string(contents)
		}
		err = sink.add(entry.Name, entry.Mode(), int64(entry.UncompressedSize64), file, target, false)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func extractTarFile(filename, format string, sink *archiveSink) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	var content io.Reader = file
	if strings.HasSuffix(format, ".gz") || strings.HasSuffix(format, ".tgz") {
		reader, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer reader.Close()
		content = reader
	} else if strings.HasSuffix(format, ".bz2") || strings.HasSuffix(format, ".tbz2") {
		content = bzip2.NewReader(file)
	}
	limited := &io.LimitedReader{R: content, N: sink.limit + 1}
	if err := extractTar(limited, sink); err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return fmt.Errorf("finish archive decompression: %w", err)
	}
	if limited.N == 0 {
		return fmt.Errorf("decompressed archive exceeds limit %d bytes", sink.limit)
	}
	return nil
}

func extractTar(content io.Reader, sink *archiveSink) error {
	reader := tar.NewReader(content)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		hard := header.Typeflag == tar.TypeLink
		if err := sink.add(header.Name, header.FileInfo().Mode(), header.Size, reader, header.Linkname, hard); err != nil {
			return err
		}
	}
}

func sevenZipCommand(b Config) (string, error) {
	if b.SzPath != "" {
		return b.SzPath, nil
	}
	for _, name := range []string{"7zz", "7z"} {
		command, err := installerCommand(b, b.TempDir, nil, name)
		if err == nil {
			return command.Path, nil
		}
		if !errors.Is(err, exec.ErrNotFound) {
			return "", err
		}
	}
	return "", fmt.Errorf("archive requires a configured extractor or preinstalled 7zz/7z; neither executable was found")
}

func extractXZ(b Config, artifact, destination string, sink *archiveSink) error {
	executable, err := sevenZipCommand(b)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), "decoded-*.tar")
	if err != nil {
		return err
	}
	writer := &boundedWriter{destination: file, remaining: extractionLimit(b) + 128<<20}
	err = streamInstallerCommand(b, filepath.Dir(destination), writer, executable, "e", "-so", "--", artifact)
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("decode xz with configured/preinstalled 7z: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	return extractTarFile(file.Name(), ".tar", sink)
}

type sevenZipEntry struct {
	name string
	size int64
	dir  bool
}

func sevenZipEntries(listing []byte) ([]sevenZipEntry, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(listing)))
	scanner.Buffer(make([]byte, 4096), 65536)
	var entries []sevenZipEntry
	var current sevenZipEntry
	started := false
	hasSize := false
	flush := func() error {
		if current.name == "" {
			return nil
		}
		if !current.dir && !hasSize {
			return fmt.Errorf("7z listing entry %q has no size", current.name)
		}
		entries = append(entries, current)
		current, hasSize = sevenZipEntry{}, false
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if !started {
			started = line == "----------"
			continue
		}
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		parts := strings.SplitN(line, " = ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid 7z listing line %q", line)
		}
		switch parts[0] {
		case "Path":
			if current.name != "" {
				return nil, fmt.Errorf("duplicate Path in 7z listing")
			}
			current.name = strings.ReplaceAll(parts[1], "\\", "/")
		case "Size":
			value, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil || value < 0 {
				return nil, fmt.Errorf("invalid 7z size %q", parts[1])
			}
			current.size, hasSize = value, true
		case "Folder":
			current.dir = parts[1] == "+"
		case "Attributes":
			current.dir = current.dir || strings.HasPrefix(parts[1], "D")
			if strings.Contains(parts[1], "l") {
				return nil, fmt.Errorf("7z symbolic links are unsupported; use zip or tar with explicit link metadata")
			}
		case "Symbolic Link", "Hard Link":
			return nil, fmt.Errorf("7z link entry %q is unsupported; use zip or tar", current.name)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if !started || len(entries) == 0 {
		return nil, fmt.Errorf("7z listing contained no archive entries")
	}
	return entries, nil
}

func extractSevenZip(b Config, artifact string, sink *archiveSink) error {
	executable, err := sevenZipCommand(b)
	if err != nil {
		return err
	}
	listing, err := captureInstallerCommand(b, sink.root, executable, "l", "-slt", "--", artifact)
	if err != nil {
		return fmt.Errorf("list archive with configured/preinstalled 7z: %w", err)
	}
	entries, err := sevenZipEntries(listing)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := archivePath(sink.root, entry.name); err != nil {
			return err
		}
		if entry.dir {
			if err := sink.add(entry.name, os.ModeDir|0755, 0, strings.NewReader(""), "", false); err != nil {
				return err
			}
			continue
		}
		if entry.size > sink.limit-sink.written {
			return fmt.Errorf("7z entry %q exceeds extraction limit %d", entry.name, sink.limit)
		}
		file, err := os.CreateTemp(filepath.Dir(sink.root), "7z-entry-")
		if err != nil {
			return err
		}
		writer := &boundedWriter{destination: file, remaining: entry.size}
		err = streamInstallerCommand(b, sink.root, writer, executable, "e", "-so", "-spd", "--", artifact, entry.name)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		content, err := os.Open(file.Name())
		if err != nil {
			return err
		}
		err = sink.add(entry.name, 0644, entry.size, content, "", false)
		closeErr = content.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
