package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	bytesPerMebibyte  = int64(1024 * 1024)
	downloadUserAgent = "Portprog/1"
)

func downloadAsset(app App, packageName string, asset PackageAsset) (string, error) {
	if err := validateDigest(asset.SHA256); err != nil {
		return "", fmt.Errorf("download package %q: %w", packageName, err)
	} else if asset.Size < 0 {
		return "", fmt.Errorf("download package %q: expected size zero or greater, got %d", packageName, asset.Size)
	} else if asset.Size > app.Settings.MaxDownloadMB*bytesPerMebibyte {
		return "", fmt.Errorf("download package %q: expected publisher size at most configured maximum of %d bytes, got %d", packageName, app.Settings.MaxDownloadMB*bytesPerMebibyte, asset.Size)
	} else if err := validateDownloadURL(asset.URL, app.AllowHTTPForTests); err != nil {
		return "", fmt.Errorf("download package %q: %w", packageName, err)
	} else {
		cachePath := filepath.Join(app.Paths.CacheObjects, asset.SHA256)
		valid, verifyErr := verifyCachedFile(cachePath, asset)
		if verifyErr != nil {
			return "", fmt.Errorf("verify cached package %q at %q: %w", packageName, cachePath, verifyErr)
		} else if valid {
			return cachePath, nil
		} else if app.Settings.Offline {
			return "", fmt.Errorf("download package %q in offline mode: expected verified cache object %q, got none", packageName, cachePath)
		} else {
			return fetchAsset(app, packageName, asset, cachePath)
		}
	}
}

func fetchAsset(app App, packageName string, asset PackageAsset, cachePath string) (string, error) {
	if err := ensureContainedDirectory(app.Paths.BaseDir, app.Paths.CacheObjects); err != nil {
		return "", fmt.Errorf("prepare cache for package %q at %q: expected writable directory, got error: %w", packageName, app.Paths.CacheObjects, err)
	} else {
		if regularFileExists(cachePath) {
			_, moveErr := movePathToTrash(app, cachePath, "invalid-cache-"+packageName)
			if moveErr != nil {
				return "", fmt.Errorf("preserve invalid cached package %q at %q: %w", packageName, cachePath, moveErr)
			} else {
				return requestAsset(app, packageName, asset, cachePath)
			}
		} else {
			return requestAsset(app, packageName, asset, cachePath)
		}
	}
}

func requestAsset(app App, packageName string, asset PackageAsset, cachePath string) (string, error) {
	request, requestErr := http.NewRequest(http.MethodGet, asset.URL, nil)
	if requestErr != nil {
		return "", fmt.Errorf("create download request for package %q from %q: expected a valid URL, got error: %w", packageName, asset.URL, requestErr)
	} else {
		request.Header.Set("User-Agent", downloadUserAgent)
		request.Header.Set("Accept-Encoding", "identity")
		response, responseErr := app.Deps.HTTPClient.Do(request)
		if responseErr != nil {
			return "", fmt.Errorf("download package %q from %q: expected HTTP 200, got request error: %w", packageName, asset.URL, responseErr)
		} else if response.Request == nil || response.Request.URL == nil {
			closeErr := response.Body.Close()
			return "", fmt.Errorf("download package %q from %q: expected a recorded final HTTPS URL, got none and body close result %v", packageName, asset.URL, closeErr)
		} else if finalURLErr := validateDownloadURL(response.Request.URL.String(), app.AllowHTTPForTests); finalURLErr != nil {
			closeErr := response.Body.Close()
			return "", fmt.Errorf("download package %q from %q: expected every redirect to remain safe, got final URL %q: %v; body close result %v", packageName, asset.URL, response.Request.URL.String(), finalURLErr, closeErr)
		} else {
			return consumeDownload(app, packageName, asset, cachePath, response)
		}
	}
}

func consumeDownload(app App, packageName string, asset PackageAsset, cachePath string, response *http.Response) (string, error) {
	maximum := app.Settings.MaxDownloadMB * bytesPerMebibyte
	if response.StatusCode != http.StatusOK {
		closeErr := response.Body.Close()
		if closeErr != nil {
			return "", fmt.Errorf("download package %q from %q: expected HTTP 200, got status %s and body close error: %v", packageName, asset.URL, response.Status, closeErr)
		} else {
			return "", fmt.Errorf("download package %q from %q: expected HTTP 200, got status %s", packageName, asset.URL, response.Status)
		}
	} else if response.ContentLength > maximum {
		closeErr := response.Body.Close()
		if closeErr != nil {
			return "", fmt.Errorf("download package %q: expected at most %d bytes, server declared %d and body close failed: %v", packageName, maximum, response.ContentLength, closeErr)
		} else {
			return "", fmt.Errorf("download package %q: expected at most the configured maximum of %d bytes, server declared %d", packageName, maximum, response.ContentLength)
		}
	} else {
		return writeDownload(app, packageName, asset, cachePath, response, maximum)
	}
}

func writeDownload(app App, packageName string, asset PackageAsset, cachePath string, response *http.Response, maximum int64) (string, error) {
	temporary, createErr := os.CreateTemp(app.Paths.CacheObjects, ".download-*")
	if createErr != nil {
		closeErr := response.Body.Close()
		return "", fmt.Errorf("create temporary download for package %q: expected writable cache, got create error %v and body close result %v", packageName, createErr, closeErr)
	} else {
		temporaryPath := temporary.Name()
		hasher := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(response.Body, maximum+1))
		syncErr := temporary.Sync()
		fileCloseErr := temporary.Close()
		bodyCloseErr := response.Body.Close()
		if copyErr != nil || syncErr != nil || fileCloseErr != nil || bodyCloseErr != nil {
			preserveTemporaryDownload(app, temporaryPath, packageName)
			return "", fmt.Errorf("save package %q download: expected complete synchronized file, got copy=%v sync=%v file-close=%v body-close=%v", packageName, copyErr, syncErr, fileCloseErr, bodyCloseErr)
		} else if written > maximum {
			preserveTemporaryDownload(app, temporaryPath, packageName)
			return "", fmt.Errorf("save package %q download: expected at most the configured maximum of %d bytes, got at least %d", packageName, maximum, written)
		} else if asset.Size != 0 && written != asset.Size {
			preserveTemporaryDownload(app, temporaryPath, packageName)
			return "", fmt.Errorf("verify package %q download size: expected %d bytes, got %d", packageName, asset.Size, written)
		} else {
			actualDigest := fmt.Sprintf("%x", hasher.Sum(nil))
			return publishDownload(app, packageName, asset, temporaryPath, cachePath, actualDigest)
		}
	}
}

func publishDownload(app App, packageName string, asset PackageAsset, temporaryPath string, cachePath string, actualDigest string) (string, error) {
	if actualDigest != asset.SHA256 {
		preserveTemporaryDownload(app, temporaryPath, packageName)
		return "", fmt.Errorf("verify package %q SHA-256: expected %s, got %s", packageName, asset.SHA256, actualDigest)
	} else if err := os.Chmod(temporaryPath, defaultFileMode); err != nil {
		preserveTemporaryDownload(app, temporaryPath, packageName)
		return "", fmt.Errorf("set cached package %q permissions on %q: expected mode %o, got error: %w", packageName, temporaryPath, defaultFileMode, err)
	} else if err := os.Rename(temporaryPath, cachePath); err != nil {
		preserveTemporaryDownload(app, temporaryPath, packageName)
		return "", fmt.Errorf("publish cached package %q from %q to %q: expected atomic rename, got error: %w", packageName, temporaryPath, cachePath, err)
	} else {
		return cachePath, nil
	}
}

func verifyCachedFile(filename string, asset PackageAsset) (bool, error) {
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect cache file %q: expected a regular file or no file, got error: %w", filename, err)
	} else if !info.Mode().IsRegular() {
		return false, fmt.Errorf("inspect cache file %q: expected a regular file, got mode %v", filename, info.Mode())
	} else if asset.Size != 0 && info.Size() != asset.Size {
		return false, nil
	} else {
		digest, digestErr := fileSHA256(filename)
		if digestErr != nil {
			return false, digestErr
		} else {
			return digest == asset.SHA256, nil
		}
	}
}

func fileSHA256(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("hash file %q: expected readable file, got error: %w", filename, err)
	} else {
		hasher := sha256.New()
		_, copyErr := io.Copy(hasher, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", fmt.Errorf("hash file %q: expected complete read, got error: %w", filename, copyErr)
		} else if closeErr != nil {
			return "", fmt.Errorf("hash file %q: expected successful close, got error: %w", filename, closeErr)
		} else {
			return fmt.Sprintf("%x", hasher.Sum(nil)), nil
		}
	}
}

func validateDownloadURL(value string, allowHTTP bool) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("validate download URL: expected HTTPS URL, got %q: %w", value, err)
	} else if parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("validate download URL: expected a host and no embedded credentials, got %q", value)
	} else if parsed.Scheme == "https" {
		return nil
	} else if allowHTTP && parsed.Scheme == "http" && strings.HasPrefix(parsed.Host, "127.0.0.1:") {
		return nil
	} else {
		return fmt.Errorf("validate download URL: expected HTTPS, got %q", value)
	}
}

func redirectPolicy(allowHTTP bool) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, prior []*http.Request) error {
		if len(prior) >= 10 {
			return fmt.Errorf("follow download redirect: expected at most 10 requests, got %d", len(prior)+1)
		} else if err := validateDownloadURL(request.URL.String(), allowHTTP); err != nil {
			return fmt.Errorf("follow download redirect to %q: %w", request.URL.String(), err)
		} else {
			return nil
		}
	}
}

func preserveTemporaryDownload(app App, filename string, packageName string) {
	_, err := movePathToTrash(app, filename, "failed-download-"+packageName)
	if err != nil {
		removeErr := os.Remove(filename)
		if removeErr != nil && !os.IsNotExist(removeErr) {
			fmt.Fprintf(app.Deps.Error, "Preserve failed download %q: trash move failed with %v and temporary-file removal failed with %v\n", filename, err, removeErr)
		} else {
			fmt.Fprintf(app.Deps.Error, "Preserve failed download %q: trash move failed with %v; newly created temporary file was removed\n", filename, err)
		}
	} else {
		return
	}
}
