package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractZipStripsTopDirectoryAndVerifiesExecutable(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "tool.zip")
	writeZipFixture(t, archivePath, []zipFixtureEntry{
		{Name: "tool-1.0/", Mode: os.ModeDir | defaultDirMode},
		{Name: "tool-1.0/bin/tool", Body: "working tool", Mode: executableFileMode},
	})
	target := filepath.Join(t.TempDir(), "target")
	asset := PackageAsset{Format: "zip", StripComponents: 1, BinDirs: []string{"bin"}, Executables: []string{"bin/tool"}}
	err := extractAsset(archivePath, target, asset, 16*bytesPerMebibyte)
	if err != nil {
		t.Fatalf("extract valid ZIP fixture: expected success, got %v", err)
	} else {
		contents, readErr := os.ReadFile(filepath.Join(target, "bin", "tool"))
		if readErr != nil {
			t.Fatalf("read extracted executable: expected success, got %v", readErr)
		} else {
			if string(contents) != "working tool" {
				t.Fatalf("check extracted executable: expected working tool, got %q", string(contents))
			} else {
				return
			}
		}
	}
}

func TestExtractRejectsTraversalAndLinks(t *testing.T) {
	traversalPath := filepath.Join(t.TempDir(), "traversal.zip")
	writeZipFixture(t, traversalPath, []zipFixtureEntry{{Name: "../escape", Body: "bad", Mode: defaultFileMode}})
	traversalErr := extractAsset(traversalPath, filepath.Join(t.TempDir(), "target"), PackageAsset{Format: "zip", Executables: []string{"escape"}}, 16*bytesPerMebibyte)
	if traversalErr == nil {
		t.Fatal("extract traversal ZIP: expected rejection, got success")
	} else {
		linkPath := filepath.Join(t.TempDir(), "link.zip")
		writeZipFixture(t, linkPath, []zipFixtureEntry{{Name: "link", Body: "target", Mode: os.ModeSymlink | 0o777}})
		linkErr := extractAsset(linkPath, filepath.Join(t.TempDir(), "target"), PackageAsset{Format: "zip", Executables: []string{"link"}}, 16*bytesPerMebibyte)
		if linkErr == nil {
			t.Fatal("extract symbolic-link ZIP: expected rejection, got success")
		} else {
			return
		}
	}
}

func TestExtractTarGzipRejectsSymbolicLink(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "tool.tar.gz")
	file, createErr := os.Create(archivePath)
	if createErr != nil {
		t.Fatalf("create tar fixture: expected success, got %v", createErr)
	} else {
		gzipWriter := gzip.NewWriter(file)
		tarWriter := tar.NewWriter(gzipWriter)
		writeErr := tarWriter.WriteHeader(&tar.Header{Name: "tool/link", Typeflag: tar.TypeSymlink, Linkname: "../../outside", Mode: 0o777})
		closeTarErr := tarWriter.Close()
		closeGzipErr := gzipWriter.Close()
		closeFileErr := file.Close()
		if writeErr != nil || closeTarErr != nil || closeGzipErr != nil || closeFileErr != nil {
			t.Fatalf("write tar fixture: expected success, got write=%v tar-close=%v gzip-close=%v file-close=%v", writeErr, closeTarErr, closeGzipErr, closeFileErr)
		} else {
			err := extractAsset(archivePath, filepath.Join(t.TempDir(), "target"), PackageAsset{Format: "tar.gz", StripComponents: 1, Executables: []string{"link"}}, 16*bytesPerMebibyte)
			if err == nil {
				t.Fatal("extract symbolic-link tar: expected rejection, got success")
			} else {
				return
			}
		}
	}
}

func TestExtractRawUsesDeclaredContainedPath(t *testing.T) {
	source := filepath.Join(t.TempDir(), "download")
	writeTestFile(t, source, "raw executable")
	target := filepath.Join(t.TempDir(), "target")
	asset := PackageAsset{Format: "raw", RawPath: "bin/raw-tool", BinDirs: []string{"bin"}, Executables: []string{"bin/raw-tool"}}
	err := extractAsset(source, target, asset, 16*bytesPerMebibyte)
	if err != nil {
		t.Fatalf("extract valid raw asset: expected success, got %v", err)
	} else {
		_, statErr := os.Stat(filepath.Join(target, "bin", "raw-tool"))
		if statErr != nil {
			t.Fatalf("find installed raw asset: expected regular file, got %v", statErr)
		} else {
			return
		}
	}
}

func TestExtractRejectsMissingDeclaredBinaryDirectory(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "tool.zip")
	writeZipFixture(t, archivePath, []zipFixtureEntry{{Name: "tool", Body: "working tool", Mode: executableFileMode}})
	asset := PackageAsset{Format: "zip", BinDirs: []string{"bin"}, Executables: []string{"tool"}}
	err := extractAsset(archivePath, filepath.Join(t.TempDir(), "target"), asset, 16*bytesPerMebibyte)
	if err == nil {
		t.Fatal("extract asset missing declared binary directory: expected rejection, got success")
	} else {
		return
	}
}

type zipFixtureEntry struct {
	Name string
	Body string
	Mode os.FileMode
}

func writeZipFixture(t *testing.T, path string, entries []zipFixtureEntry) {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.Name, Method: zip.Deflate}
		header.SetMode(entry.Mode)
		entryWriter, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create ZIP fixture entry %q: expected success, got %v", entry.Name, err)
		} else {
			_, writeErr := entryWriter.Write([]byte(entry.Body))
			if writeErr != nil {
				t.Fatalf("write ZIP fixture entry %q: expected success, got %v", entry.Name, writeErr)
			} else {
				continue
			}
		}
	}
	closeErr := writer.Close()
	if closeErr != nil {
		t.Fatalf("close ZIP fixture: expected success, got %v", closeErr)
	} else {
		writeErr := os.WriteFile(path, buffer.Bytes(), defaultFileMode)
		if writeErr != nil {
			t.Fatalf("write ZIP fixture %q: expected success, got %v", path, writeErr)
		} else {
			return
		}
	}
}
