// Copyright The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v2"
)

type FortiExporterParameter struct {
	AuthFile      *string
	Listen        *string
	ScrapeTimeout *int
	TLSTimeout    *int
	TLSInsecure   *bool
	TLSExtraCAs   *string
	MaxBGPPaths   *int
	MaxVPNUsers   *int

	MaxConcurrency     *int
	UnavailableRecheck *time.Duration
}

type FortiExporterConfig struct {
	AuthKeys      AuthKeys
	Listen        string
	ScrapeTimeout int
	TLSTimeout    int
	TLSInsecure   bool
	TLSExtraCAs   []LocalCert
	MaxBGPPaths   int
	MaxVPNUsers   int

	// MaxConcurrency is the number of probes executed in parallel against a
	// single target. Values below 1 mean sequential execution.
	MaxConcurrency int
	// UnavailableRecheck is how long a probe whose API endpoint is not
	// available on a target (e.g. HTTP 404, or BGP/OSPF not configured) is
	// skipped before it is tried again. 0 means: try again on every scrape.
	UnavailableRecheck time.Duration
}

type AuthKeys map[Target]TargetAuth

type (
	Target    string
	Token     string
	ProbeList []string
)

type Probes struct {
	Include ProbeList
	Exclude ProbeList
}

type TargetAuth struct {
	Token  Token
	Probes Probes
}

type LocalCert struct {
	Path    string
	Content []byte
}

var (
	parameter = FortiExporterParameter{
		AuthFile:      flag.String("auth-file", "fortigate-key.yaml", "file containing the authentication map to use when connecting to a Fortigate device"),
		Listen:        flag.String("listen", ":9710", "address to listen on"),
		ScrapeTimeout: flag.Int("scrape-timeout", 30, "max seconds to allow a scrape to take"),
		TLSTimeout:    flag.Int("https-timeout", 10, "TLS Handshake timeout in seconds"),
		TLSInsecure:   flag.Bool("insecure", false, "Allow insecure certificates"),
		TLSExtraCAs:   flag.String("extra-ca-certs", "", "comma-separated files containing extra PEMs to trust for TLS connections in addition to the system trust store"),
		MaxBGPPaths:   flag.Int("max-bgp-paths", 10000, "How many BGP Paths to receive when counting routes, needs to be greater than or equal to the number of routes or metrics will not be generated"),
		MaxVPNUsers:   flag.Int("max-vpn-users", 0, "How many VPN Users to receive when counting users, needs to be greater than or equal the number of users or metrics will not be generated (0 eq. none by default)"),

		MaxConcurrency:     flag.Int("max-concurrency", 4, "How many probes to run in parallel against a single target (1 = sequential)"),
		UnavailableRecheck: flag.Duration("unavailable-recheck-interval", time.Hour, "How long to skip probes whose API endpoint is not available on a target (e.g. HTTP 404, BGP/OSPF not configured) before trying again (0 = try on every scrape)"),
	}

	savedConfig *FortiExporterConfig
)

func Init() error {
	// check if already parsed
	if savedConfig != nil {
		return nil
	}
	return ReInit()
}

func MustReInit() {
	if err := ReInit(); err != nil {
		log.Fatalf("config.ReInit failed: %+v", err)
	}
}

func ReInit() error {
	flag.Parse()

	savedConfig = &FortiExporterConfig{
		Listen:        *parameter.Listen,
		ScrapeTimeout: *parameter.ScrapeTimeout,
		TLSTimeout:    *parameter.TLSTimeout,
		TLSInsecure:   *parameter.TLSInsecure,
		MaxBGPPaths:   *parameter.MaxBGPPaths,
		MaxVPNUsers:   *parameter.MaxVPNUsers,

		MaxConcurrency:     *parameter.MaxConcurrency,
		UnavailableRecheck: *parameter.UnavailableRecheck,
	}

	// parse AuthKeys
	af, err := os.ReadFile(*parameter.AuthFile)
	if err != nil {
		log.Fatalf("Failed to read API authentication map file: %v", err)
		return err
	}

	if err := yaml.Unmarshal(af, &savedConfig.AuthKeys); err != nil {
		log.Fatalf("Failed to parse API authentication map file: %v", err)
		return err
	}

	log.Printf("Loaded %d API keys", len(savedConfig.AuthKeys))

	// parse ExtraCAs
	for eca := range strings.SplitSeq(*parameter.TLSExtraCAs, ",") {
		if eca == "" {
			continue
		}

		certs, err := os.ReadFile(eca)
		if err != nil {
			log.Fatalf("Failed to read extra CA file %q: %v", eca, err)
			return err
		}

		certObject := LocalCert{
			Path:    eca,
			Content: certs,
		}
		savedConfig.TLSExtraCAs = append(savedConfig.TLSExtraCAs, certObject)
	}

	return nil
}

func GetConfig() FortiExporterConfig {
	return *savedConfig
}
