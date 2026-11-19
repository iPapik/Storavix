package collector

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"debug/pe"
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

const maxRAIDToolBytes = 128 << 20

var megaToolNames = []string{"storcli2", "perccli2", "perccli", "storcli"}
var hpeToolNames = []string{"ssacli"}

func raidToolName(name string) (string, error) {
	if !contains(megaToolNames, name) && !contains(hpeToolNames, name) {
		return "", errors.New("expected storcli, storcli2, perccli, perccli2, or ssacli")
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name, nil
}

func managedRAIDTools(names []string) []string {
	dir := raidToolDirectory()
	if !filepath.IsAbs(dir) {
		return nil
	}
	var paths []string
	for _, name := range names {
		binary, _ := raidToolName(name)
		paths = append(paths, filepath.Join(dir, binary))
	}
	return paths
}

func installRAIDToolForHardware(name string, devices []raidDevice) bool {
	for _, device := range devices {
		if !device.RAID && !isRAIDDriver(device.Driver) {
			continue
		}
		switch device.family() {
		case "mega":
			dell := device.Vendor == "1028" || strings.HasPrefix(device.Subsystem, "1028:") ||
				strings.Contains(strings.ToLower(device.Model), "perc h")
			if dell && strings.HasPrefix(name, "perccli") || !dell && strings.HasPrefix(name, "storcli") {
				return true
			}
		case "hpe":
			if name == "ssacli" {
				return true
			}
		}
	}
	return false
}

// InstallRAIDTool stages only the approved portable CLI. No package scripts or
// executables from the input are run during provisioning.
func InstallRAIDTool(name, source, checksum string, onlyMatchingHardware bool) (string, error) {
	binary, err := raidToolName(name)
	if err != nil {
		return "", err
	}
	if !validToolSHA256(checksum) {
		return "", errors.New("expected the SHA-256 of the portable RAID executable")
	}
	if source == "" {
		return "", errors.New("RAID executable source is required")
	}
	if onlyMatchingHardware && !installRAIDToolForHardware(name, detectedRAIDDevices()) {
		return "", nil
	}
	dir := raidToolDirectory()
	if !filepath.IsAbs(dir) {
		return "", errors.New("managed RAID tool directory unavailable on this host")
	}
	if err := prepareRAIDToolDirectory(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, binary)
	if current, err := os.Open(path); err == nil {
		matches := verifyRAIDToolChecksum(current, checksum) == nil
		_ = current.Close()
		if matches {
			return path, nil
		}
	}
	if err := stageRAIDTool(path, source, checksum, verifyNativeRAIDFormat); err != nil {
		return "", err
	}
	return path, nil
}

func validToolSHA256(checksum string) bool {
	_, err := hex.DecodeString(checksum)
	return len(checksum) == sha256.Size*2 && err == nil
}

func verifyRAIDToolChecksum(src io.Reader, checksum string) error {
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(src, maxRAIDToolBytes+1))
	if err != nil {
		return err
	}
	if n == 0 || n > maxRAIDToolBytes {
		return errors.New("invalid RAID executable size")
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), checksum) {
		return errors.New("RAID executable SHA-256 mismatch")
	}
	return nil
}

func stageRAIDTool(path, source, checksum string, verifyFormat func(string) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	src, err := openRAIDTool(ctx, source)
	if err != nil {
		return err
	}
	defer src.Close()
	temp, err := os.CreateTemp(filepath.Dir(path), ".raid-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err = verifyRAIDToolChecksum(io.TeeReader(src, temp), checksum); err == nil {
		err = temp.Chmod(0755)
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := verifyFormat(temp.Name()); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func openRAIDTool(ctx context.Context, source string) (io.ReadCloser, error) {
	if filepath.IsAbs(source) {
		info, err := os.Stat(source)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > maxRAIDToolBytes {
			return nil, errors.New("expected a regular RAID executable")
		}
		return os.Open(source)
	}

	validURL := func(u *url.URL) bool {
		return u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == ""
	}
	u, err := url.Parse(source)
	if err != nil || !validURL(u) {
		return nil, errors.New("expected an absolute file path or HTTPS URL for the RAID executable")
	}
	client := &http.Client{Timeout: 100 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !validURL(r.URL) {
			return errors.New("RAID executable redirect refused")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK || resp.ContentLength > maxRAIDToolBytes {
		resp.Body.Close()
		return nil, fmt.Errorf("unable to download RAID executable: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func verifyNativeRAIDFormat(path string) error {
	switch runtime.GOOS {
	case "linux":
		binary, err := elf.Open(path)
		if err == nil {
			defer binary.Close()
			if binary.Type == elf.ET_EXEC || binary.Type == elf.ET_DYN {
				if binary.Machine == elf.EM_X86_64 && runtime.GOARCH == "amd64" ||
					binary.Machine == elf.EM_AARCH64 && runtime.GOARCH == "arm64" {
					return nil
				}
			}
		}
	case "windows":
		binary, err := pe.Open(path)
		if err == nil {
			defer binary.Close()
			if binary.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE != 0 && binary.Characteristics&pe.IMAGE_FILE_DLL == 0 &&
				(binary.Machine == pe.IMAGE_FILE_MACHINE_AMD64 && runtime.GOARCH == "amd64" ||
					binary.Machine == pe.IMAGE_FILE_MACHINE_ARM64 && runtime.GOARCH == "arm64") {
				return nil
			}
		}
	}
	return fmt.Errorf("expected a portable RAID executable for %s/%s (extract it from the vendor package first)", runtime.GOOS, runtime.GOARCH)
}
