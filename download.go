package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func downloadLimit(b Config) int64 {
	if b.MaxDownloadBytes > 0 {
		return b.MaxDownloadBytes
	}
	return defaultSettings().MaxDownloadBytes
}

func extractionLimit(b Config) int64 {
	if b.MaxExtractBytes > 0 {
		return b.MaxExtractBytes
	}
	return defaultSettings().MaxExtractBytes
}

func validDigest(digest string) bool {
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == sha256.Size && digest == strings.ToLower(digest)
}

func verifyArtifact(filename, digest string, expectedSize int64, limit int64) error {
	if !validDigest(digest) {
		return fmt.Errorf("artifact %q has invalid SHA256 %q: expected 64 lowercase hexadecimal characters", filename, digest)
	}
	entry, err := os.Lstat(filename)
	if err != nil {
		return fmt.Errorf("inspect cached artifact %q: %w", filename, err)
	}
	if !entry.Mode().IsRegular() {
		return fmt.Errorf("cached artifact %q must be a regular file, received %s", filename, entry.Mode())
	}
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open cached artifact %q: %w", filename, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect cached artifact %q: %w", filename, err)
	}
	if !info.Mode().IsRegular() || info.Size() > limit || (expectedSize >= 0 && info.Size() != expectedSize) {
		return fmt.Errorf("cached artifact %q: received mode %s and size %d; expected regular file, size %d, limit %d", filename, info.Mode(), info.Size(), expectedSize, limit)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, limit+1)); err != nil {
		return fmt.Errorf("hash cached artifact %q: %w", filename, err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != digest {
		return fmt.Errorf("cached artifact %q: SHA256 received %s, expected %s", filename, actual, digest)
	}
	return nil
}

func cacheArtifact(b Config, source io.Reader, expected string) (string, int64, error) {
	if expected != "" && !validDigest(expected) {
		return "", 0, fmt.Errorf("invalid expected SHA256 %q", expected)
	}
	if err := os.MkdirAll(b.ZipDir, 0755); err != nil {
		return "", 0, fmt.Errorf("create artifact cache %q: %w", b.ZipDir, err)
	}
	file, err := os.CreateTemp(b.ZipDir, ".download-")
	if err != nil {
		return "", 0, fmt.Errorf("create artifact download in %q: %w", b.ZipDir, err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(source, downloadLimit(b)+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return "", 0, fmt.Errorf("download retained at %q after %d bytes: %w", file.Name(), written, copyErr)
	}
	if written > downloadLimit(b) {
		return "", 0, fmt.Errorf("download retained at %q: received more than %d bytes", file.Name(), downloadLimit(b))
	}
	if syncErr != nil {
		return "", 0, fmt.Errorf("flush downloaded artifact %q: %w", file.Name(), syncErr)
	}
	if closeErr != nil {
		return "", 0, fmt.Errorf("close downloaded artifact %q: %w", file.Name(), closeErr)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if expected != "" && digest != expected {
		return "", 0, fmt.Errorf("download retained at %q: SHA256 received %s, expected %s", file.Name(), digest, expected)
	}
	destination := filepath.Join(b.ZipDir, digest)
	if _, err := os.Lstat(destination); err == nil {
		if err := verifyArtifact(destination, digest, written, downloadLimit(b)); err != nil {
			return "", 0, err
		}
		return digest, written, nil
	} else if !os.IsNotExist(err) {
		return "", 0, fmt.Errorf("inspect artifact destination %q: %w", destination, err)
	}
	if err := os.Rename(file.Name(), destination); err != nil {
		return "", 0, fmt.Errorf("publish downloaded artifact %q as %q: %w", file.Name(), destination, err)
	}
	return digest, written, nil
}

func localArtifactPath(address *url.URL, windows bool) (string, error) {
	filename := address.Path
	if windows {
		if len(filename) < 4 || filename[0] != '/' || filename[2] != ':' || filename[3] != '/' || !((filename[1] >= 'A' && filename[1] <= 'Z') || (filename[1] >= 'a' && filename[1] <= 'z')) {
			return "", fmt.Errorf("Windows file URL %q must include an absolute drive path such as file:///C:/packages/archive.zip", address)
		}
		return strings.ReplaceAll(filename[1:], "/", "\\"), nil
	}
	if !filepath.IsAbs(filename) {
		return "", fmt.Errorf("file URL %q must contain an absolute local path", address)
	}
	return filepath.FromSlash(filename), nil
}

func artifactURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse artifact URL %q: %w", raw, err)
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return nil, fmt.Errorf("artifact URL %q must not contain credentials or a fragment", raw)
	}
	if parsed.Scheme == "https" && parsed.Host != "" {
		return parsed, nil
	}
	if parsed.Scheme == "file" && (parsed.Host == "" || parsed.Host == "localhost") && parsed.RawQuery == "" {
		if _, err := localArtifactPath(parsed, runtime.GOOS == "windows"); err == nil {
			return parsed, nil
		}
	}
	return nil, fmt.Errorf("artifact URL %q must use HTTPS or an absolute local file:// URL", raw)
}

func resolvePackage(b Config, p Package) (LockedPackage, error) {
	locked := LockedPackage{Package: p}
	commit, commitErr := hex.DecodeString(p.Branch)
	exactCommit := commitErr == nil && (len(commit) == 20 || len(commit) == 32)
	if p.SHA256 != "" {
		if !validDigest(p.SHA256) {
			return locked, fmt.Errorf("package %q: invalid SHA256 %q", p.Name, p.SHA256)
		}
		filename := filepath.Join(b.ZipDir, p.SHA256)
		if err := verifyArtifact(filename, p.SHA256, -1, downloadLimit(b)); err == nil {
			info, err := os.Stat(filename)
			if err != nil {
				return locked, err
			}
			if p.Fetch != "git" || exactCommit {
				locked.SHA256, locked.Size = p.SHA256, info.Size()
				if p.Fetch == "git" {
					locked.Commit = p.Branch
				}
				return locked, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return locked, err
		}
	}
	if b.Offline {
		return locked, fmt.Errorf("package %q cannot be resolved offline: verified cache entry for SHA256 %q is unavailable", p.Name, p.SHA256)
	}
	parsed, err := artifactURL(p.Url)
	if err != nil {
		return locked, err
	}
	if p.Fetch == "git" {
		return resolveGitPackage(b, p, parsed)
	}
	if p.Fetch != "web" {
		return locked, fmt.Errorf("package %q has unsupported fetch method %q", p.Name, p.Fetch)
	}
	if parsed.Scheme == "file" {
		filename, err := localArtifactPath(parsed, runtime.GOOS == "windows")
		if err != nil {
			return locked, err
		}
		file, err := os.Open(filename)
		if err != nil {
			return locked, fmt.Errorf("open local artifact %q: %w", parsed.Path, err)
		}
		locked.SHA256, locked.Size, err = cacheArtifact(b, file, p.SHA256)
		closeErr := file.Close()
		if err == nil && closeErr != nil {
			err = fmt.Errorf("close local artifact %q: %w", parsed.Path, closeErr)
		}
		return locked, err
	}
	timeout := b.HTTPTimeout
	if timeout <= 0 {
		timeout = time.Duration(defaultSettings().HTTPTimeoutSecs) * time.Second
	}
	client := &http.Client{Timeout: timeout, CheckRedirect: func(request *http.Request, previous []*http.Request) error {
		if request.URL.Scheme != "https" || len(previous) >= 10 {
			return fmt.Errorf("refuse redirect to %q after %d redirects: HTTPS and at most 10 redirects required", request.URL, len(previous))
		}
		return nil
	}}
	locked.SHA256, locked.Size, err = fetchArtifact(b, client, p.Url, p.SHA256)
	return locked, err
}

func fetchArtifact(b Config, client *http.Client, address, digest string) (string, int64, error) {
	if b.Offline {
		return "", 0, fmt.Errorf("network request to %q refused in offline mode", address)
	}
	response, err := client.Get(address)
	if err != nil {
		return "", 0, fmt.Errorf("download %q: %w", address, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return "", 0, fmt.Errorf("download %q: received HTTP %s; expected 2xx", address, response.Status)
	}
	if response.ContentLength > downloadLimit(b) {
		response.Body.Close()
		return "", 0, fmt.Errorf("download %q: content length %d exceeds limit %d", address, response.ContentLength, downloadLimit(b))
	}
	resolved, size, err := cacheArtifact(b, response.Body, digest)
	closeErr := response.Body.Close()
	if err != nil {
		return "", 0, err
	}
	if closeErr != nil {
		return "", 0, fmt.Errorf("close response body from %q: %w", address, closeErr)
	}
	return resolved, size, nil
}

func resolveGitPackage(b Config, p Package, address *url.URL) (LockedPackage, error) {
	locked := LockedPackage{Package: p}
	if p.Branch == "" || strings.HasPrefix(p.Branch, "-") {
		return locked, fmt.Errorf("Git package %q requires an explicit branch, tag, or commit", p.Name)
	}
	if err := os.MkdirAll(b.TempDir, 0755); err != nil {
		return locked, err
	}
	stage, err := os.MkdirTemp(b.TempDir, "git-source-")
	if err != nil {
		return locked, err
	}
	source := filepath.Join(stage, "source")
	options := []string{"-c", "protocol.allow=never", "-c", "protocol.https.allow=always"}
	if address.Scheme == "file" {
		options = append(options, "-c", "protocol.file.allow=always")
	}
	clone := append(append([]string{}, options...), "clone", "--no-checkout", "--no-hardlinks", "--", p.Url, source)
	if err := runInstallerCommand(b, stage, "git", clone...); err != nil {
		return locked, err
	}
	var commit string
	for _, revision := range []string{"refs/remotes/origin/" + p.Branch, "refs/tags/" + p.Branch, p.Branch} {
		output, err := captureInstallerCommand(b, source, "git", "rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
		if err == nil {
			commit = strings.TrimSpace(string(output))
			break
		}
	}
	if len(commit) != 40 && len(commit) != 64 {
		return locked, fmt.Errorf("Git package %q: cannot resolve branch/tag/commit %q in %q", p.Name, p.Branch, source)
	}
	if _, err := hex.DecodeString(commit); err != nil {
		return locked, fmt.Errorf("Git package %q returned invalid commit %q", p.Name, commit)
	}
	if err := runInstallerCommand(b, source, "git", "-c", "advice.detachedHead=false", "checkout", "--detach", commit); err != nil {
		return locked, err
	}
	update := append(append([]string{}, options...), "submodule", "update", "--init", "--recursive")
	if err := runInstallerCommand(b, source, "git", update...); err != nil {
		return locked, err
	}
	snapshot, err := os.CreateTemp(stage, "snapshot-*.zip")
	if err != nil {
		return locked, err
	}
	writeErr := snapshotSource(source, snapshot, extractionLimit(b))
	closeErr := snapshot.Close()
	if writeErr != nil {
		return locked, writeErr
	}
	if closeErr != nil {
		return locked, closeErr
	}
	file, err := os.Open(snapshot.Name())
	if err != nil {
		return locked, err
	}
	locked.SHA256, locked.Size, err = cacheArtifact(b, file, p.SHA256)
	closeErr = file.Close()
	if err == nil && closeErr != nil {
		err = closeErr
	}
	locked.Commit = commit
	return locked, err
}

func snapshotSource(root string, output io.Writer, limit int64) error {
	writer := zip.NewWriter(output)
	var total int64
	err := filepath.WalkDir(root, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == root {
			return nil
		}
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(name)
		header.Modified = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
		header.Method = zip.Deflate
		if entry.IsDir() {
			header.Name += "/"
		}
		part, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filename)
			if err != nil {
				return err
			}
			if err := validateLink(root, name, target); err != nil {
				return err
			}
			_, err = io.WriteString(part, target)
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Git source %q has unsupported file mode %s", filename, info.Mode())
		}
		total += info.Size()
		if total > limit {
			return fmt.Errorf("Git source exceeds extraction limit %d bytes", limit)
		}
		file, err := os.Open(filename)
		if err != nil {
			return err
		}
		_, err = io.Copy(part, file)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	closeErr := writer.Close()
	if err != nil {
		return err
	}
	return closeErr
}
