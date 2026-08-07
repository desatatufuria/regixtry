package linux

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

const systemdRuntimePath = "/run/systemd/system"

type HostInfo struct {
	Distribution string
	VersionID    string
}

type detector struct {
	goos     string
	readFile func(string) ([]byte, error)
	stat     func(string) (os.FileInfo, error)
}

func newDetector() detector {
	return detector{
		goos:     runtime.GOOS,
		readFile: os.ReadFile,
		stat:     os.Stat,
	}
}

func (d detector) Detect() (HostInfo, error) {
	if d.goos != "linux" {
		return HostInfo{}, fmt.Errorf("unsupported operating system %q: bootstrap requires Linux", d.goos)
	}

	osRelease, err := d.readFile("/etc/os-release")
	if err != nil {
		return HostInfo{}, fmt.Errorf("read /etc/os-release: %w", err)
	}

	id, versionID, err := parseOSRelease(osRelease)
	if err != nil {
		return HostInfo{}, err
	}

	distribution, err := supportedDistribution(id, versionID)
	if err != nil {
		return HostInfo{}, err
	}

	if _, err := d.stat(systemdRuntimePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return HostInfo{}, fmt.Errorf("systemd runtime not detected at %s", systemdRuntimePath)
		}
		return HostInfo{}, fmt.Errorf("stat %s: %w", systemdRuntimePath, err)
	}

	return HostInfo{Distribution: distribution, VersionID: versionID}, nil
}

func parseOSRelease(contents []byte) (string, string, error) {
	values := map[string]string{}
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}

	id := strings.ToLower(strings.TrimSpace(values["ID"]))
	versionID := strings.TrimSpace(values["VERSION_ID"])
	if id == "" {
		return "", "", errors.New("/etc/os-release is missing ID")
	}
	if versionID == "" {
		return "", "", fmt.Errorf("/etc/os-release is missing VERSION_ID for %s", id)
	}

	return id, versionID, nil
}

func supportedDistribution(id string, versionID string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "debian":
		return "Debian", nil
	case "ubuntu":
		return "Ubuntu", nil
	case "linuxmint":
		return "Linux Mint", nil
	case "rhel":
		major := versionID
		if idx := strings.IndexRune(major, '.'); idx >= 0 {
			major = major[:idx]
		}
		if major == "9" || major == "10" {
			return "RHEL", nil
		}
		return "", fmt.Errorf("unsupported RHEL version %q: expected 9.x or 10.x", versionID)
	case "alpine":
		return "", errors.New("unsupported Linux distribution \"alpine\": Alpine host bootstrap is deferred")
	default:
		return "", fmt.Errorf("unsupported Linux distribution %q", id)
	}
}
