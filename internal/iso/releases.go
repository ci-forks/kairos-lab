package iso

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	releasesAPIURL = "https://api.github.com/repos/kairos-io/kairos/releases/latest"
)

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type ISOOption struct {
	Name        string
	DownloadURL string
	Size        int64
	Flavor      string // "core" or "standard"
	Arch        string // "amd64" or "arm64"
	// K8sDistro and K8sVersion are empty on a core image. On a standard one
	// they name the Kubernetes distribution the image ships, "k3s" or "k0s",
	// and its version, e.g. "v1.36.4+k3s1".
	K8sDistro  string
	K8sVersion string
}

// KubernetesOption is one Kubernetes distribution and version that a release
// ships a standard image for.
type KubernetesOption struct {
	Distro  string
	Version string
}

// Label is what the picker shows for this option, e.g. "k3s v1.36.4+k3s1".
func (k KubernetesOption) Label() string {
	return k.Distro + " " + k.Version
}

func FetchLatestRelease() (*Release, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(releasesAPIURL)
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch releases: unexpected status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}
	return &release, nil
}

// isoNamePattern matches a released image name. The optional trailing group is
// the Kubernetes distribution a standard image ships: k3s stamps its build as
// `+k3s<n>` and k0s as `+k0s.<n>`, so both spellings are accepted rather than
// only the k3s one.
var isoNamePattern = regexp.MustCompile(`^kairos-hadron-[^-]+-(\w+)-(amd64|arm64)-generic-v[\d.]+(-(k3s|k0s)(v[\d.]+\+(?:k3s\d+|k0s\.\d+)))?\.iso$`)

func ParseISOAssets(release *Release) []ISOOption {
	var options []ISOOption
	for _, asset := range release.Assets {
		if !strings.HasSuffix(asset.Name, ".iso") {
			continue
		}
		matches := isoNamePattern.FindStringSubmatch(asset.Name)
		if matches == nil {
			continue
		}
		// The prefix group and the build-stamp group are independent terms
		// of the pattern, so on its own it also matches a name that names
		// one distribution and stamps the other -- `...-k3sv1.2.3+k0s.0`.
		// No release publishes such a name, but the pair is what everything
		// downstream keys on: the picker, the lookup and the dedupe all read
		// distro and version together, so a name that disagrees with itself
		// would seed a k0s version under the k3s heading. RE2 has no
		// backreference to express the agreement in the pattern, so it is
		// checked here, where it can say why.
		if matches[4] != "" && !strings.Contains(matches[5], "+"+matches[4]) {
			continue
		}
		opt := ISOOption{
			Name:        asset.Name,
			DownloadURL: asset.BrowserDownloadURL,
			Size:        asset.Size,
			Flavor:      matches[1],
			Arch:        matches[2],
			K8sDistro:   matches[4],
			K8sVersion:  matches[5],
		}
		options = append(options, opt)
	}
	return options
}

func FilterByArch(options []ISOOption, arch string) []ISOOption {
	if arch == "" {
		arch = runtime.GOARCH
	}
	var filtered []ISOOption
	for _, opt := range options {
		if opt.Arch == arch {
			filtered = append(filtered, opt)
		}
	}
	return filtered
}

func FilterByFlavor(options []ISOOption, flavor string) []ISOOption {
	var filtered []ISOOption
	for _, opt := range options {
		if opt.Flavor == flavor {
			filtered = append(filtered, opt)
		}
	}
	return filtered
}

// distroRank keeps k3s at the top of the picker, where it was when it was the
// only distribution kairos-lab could see. Every other distribution follows it,
// ordered by name.
func distroRank(distro string) int {
	if distro == "k3s" {
		return 0
	}
	return 1
}

// GetKubernetesOptions lists every distribution and version the given images
// offer, newest version first within a distribution.
func GetKubernetesOptions(options []ISOOption) []KubernetesOption {
	seen := make(map[KubernetesOption]bool)
	var found []KubernetesOption
	for _, opt := range options {
		if opt.K8sVersion == "" {
			continue
		}
		k := KubernetesOption{Distro: opt.K8sDistro, Version: opt.K8sVersion}
		if !seen[k] {
			seen[k] = true
			found = append(found, k)
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if ri, rj := distroRank(found[i].Distro), distroRank(found[j].Distro); ri != rj {
			return ri < rj
		}
		if found[i].Distro != found[j].Distro {
			return found[i].Distro < found[j].Distro
		}
		return found[i].Version > found[j].Version
	})
	return found
}

func FindByKubernetes(options []ISOOption, k KubernetesOption) *ISOOption {
	for _, opt := range options {
		if opt.K8sDistro == k.Distro && opt.K8sVersion == k.Version {
			return &opt
		}
	}
	return nil
}

func FindCore(options []ISOOption) *ISOOption {
	for _, opt := range options {
		if opt.Flavor == "core" {
			return &opt
		}
	}
	return nil
}
