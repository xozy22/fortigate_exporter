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

// All currently supported probes
//
// Copyright (C) 2020  Christian Svensson
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package probe

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/prometheus-community/fortigate_exporter/internal/config"
	"github.com/prometheus-community/fortigate_exporter/internal/fortiversion"
	fortiHTTP "github.com/prometheus-community/fortigate_exporter/pkg/fortigatehttpclient"
)

type Collector struct {
	metrics []prometheus.Metric
}

type TargetMetadata struct {
	VersionMajor int
	VersionMinor int
}

type probeFunc func(fortiHTTP.FortiHTTP, *TargetMetadata) ([]prometheus.Metric, bool)

type probeDetailedFunc struct {
	name     string
	function probeFunc
	// unavailableCodes lists HTTP status codes that - for this probe only -
	// mean "the feature is not configured on the target" in addition to
	// defaultUnavailableCodes. FortiOS answers e.g. the BGP and OSPF monitor
	// endpoints with HTTP 500 when the routing protocol is not configured.
	unavailableCodes []int
}

// defaultUnavailableCodes are HTTP status codes that mean "this API endpoint
// does not exist on this model/version or the feature is disabled". Probes
// failing only with these codes are reported as unavailable instead of failed.
var defaultUnavailableCodes = []int{http.StatusNotFound, http.StatusFailedDependency}

// routingUnavailableCodes is used by the BGP and OSPF probes.
var routingUnavailableCodes = []int{http.StatusInternalServerError}

func availableProbes() []probeDetailedFunc {
	return []probeDetailedFunc{
		// Keep probeSystemTime first: probes are started in list order, so the
		// time probe is processed first and its value is closest to the
		// scrape timestamp Prometheus assigns.
		{name: "System/Time/Clock", function: probeSystemTime},
		{name: "BGP/NeighborPaths/IPv4", function: probeBGPNeighborPathsIPv4, unavailableCodes: routingUnavailableCodes},
		{name: "BGP/NeighborPaths/IPv6", function: probeBGPNeighborPathsIPv6, unavailableCodes: routingUnavailableCodes},
		{name: "BGP/Neighbors/IPv4", function: probeBGPNeighborsIPv4, unavailableCodes: routingUnavailableCodes},
		{name: "BGP/Neighbors/IPv6", function: probeBGPNeighborsIPv6, unavailableCodes: routingUnavailableCodes},
		{name: "Firewall/LoadBalance", function: probeFirewallLoadBalance},
		{name: "Firewall/Policies", function: probeFirewallPolicies},
		{name: "Firewall/IPPool", function: probeFirewallIPPool},
		{name: "License/Status", function: probeLicenseStatus},
		{name: "Log/Fortianalyzer/Status", function: probeLogAnalyzer},
		{name: "Log/Fortianalyzer/Queue", function: probeLogAnalyzerQueue},
		{name: "Log/DiskUsage", function: probeLogCurrentDiskUsage},
		{name: "Network/Dns/Latency", function: probeNetworkDNSLatency},
		{name: "System/AvailableCertificates", function: probeSystemAvailableCertificates},
		{name: "System/Central-Management/Status", function: probeSystemCentralManagementStatus},
		{name: "System/Fortimanager/Status", function: probeSystemFortimanagerStatus},
		{name: "System/Global/Location", function: probeSystemGlobalLocation},
		{name: "System/HAStatistics", function: probeSystemHAStatistics},
		{name: "System/Ha-peer", function: probeSystemHaPeer},
		{name: "System/Interface", function: probeSystemInterface},
		{name: "System/Interface/Transceivers", function: probeSystemInterfaceTransceivers},
		{name: "System/LinkMonitor", function: probeSystemLinkMonitor},
		{name: "System/Performance/Status", function: probeSystemPerformanceStatus},
		{name: "System/Ntp/Status", function: probeSystemNtpStatus},
		{name: "System/Resource/Usage", function: probeSystemResourceUsage},
		{name: "System/Resource/Usage/VDOM", function: probeSystemResourceUsagePerVdom},
		{name: "System/SDNConnector", function: probeSystemSDNConnector},
		{name: "System/SensorInfo", function: probeSystemSensorInfo},
		{name: "System/Status", function: probeSystemStatus},
		{name: "System/VDOMResource", function: probeSystemVdomResource},
		{name: "System/HAChecksum", function: probeSystemHAChecksum},
		{name: "User/Fsso", function: probeUserFsso},
		{name: "VPN/IPSec", function: probeVPNIPSec},
		{name: "VPN/Ssl/Connections", function: probeVPNSsl},
		{name: "VPN/Ssl/Stats", function: probeVPNSslStats},
		{name: "VirtualWAN/HealthCheck", function: probeVirtualWANHealthCheck},
		{name: "WebUI/State", function: probeWebUIState},
		{name: "Wifi/APStatus", function: probeWifiAPStatus},
		{name: "Wifi/Clients", function: probeWifiClients},
		{name: "Wifi/ManagedAP", function: probeWifiManagedAP},
		{name: "Switch/ManagedSwitch", function: probeManagedSwitch},
		{name: "OSPF/Neighbors", function: probeOSPFNeighbors, unavailableCodes: routingUnavailableCodes},
	}
}

// selectProbes applies the include/exclude prefix lists of a target.
func selectProbes(all []probeDetailedFunc, include, exclude config.ProbeList) []probeDetailedFunc {
	selected := make([]probeDetailedFunc, 0, len(all))
	for _, p := range all {
		wanted := len(include) == 0
		for _, prefix := range include {
			if strings.HasPrefix(p.name, prefix) {
				wanted = true
				break
			}
		}
		for _, prefix := range exclude {
			if strings.HasPrefix(p.name, prefix) {
				wanted = false
				break
			}
		}
		if wanted {
			selected = append(selected, p)
		}
	}
	return selected
}

type probeOutcome int

const (
	outcomeSuccess probeOutcome = iota
	outcomeFailure
	outcomeUnavailable
)

type probeResult struct {
	metrics    []prometheus.Metric
	outcome    probeOutcome
	duration   time.Duration
	statusCode int  // HTTP status that made the probe unavailable
	cached     bool // probe was not executed, known to be unavailable
}

// recordingClient wraps the FortiGate client of a single probe run and
// remembers which errors the probe ran into, so that a failed probe can be
// classified as "unavailable" (feature/endpoint missing) or "failure".
type recordingClient struct {
	inner       fortiHTTP.FortiHTTP
	unavailable map[int]bool

	mu              sync.Mutex
	unavailableCode int
	otherError      bool
}

func newRecordingClient(inner fortiHTTP.FortiHTTP, extraCodes []int) *recordingClient {
	codes := make(map[int]bool, len(defaultUnavailableCodes)+len(extraCodes))
	for _, c := range defaultUnavailableCodes {
		codes[c] = true
	}
	for _, c := range extraCodes {
		codes[c] = true
	}
	return &recordingClient{inner: inner, unavailable: codes}
}

func (r *recordingClient) Get(path, query string, obj any) error {
	err := r.inner.Get(path, query, obj)
	if err == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if code, ok := fortiHTTP.StatusCode(err); ok && r.unavailable[code] {
		r.unavailableCode = code
	} else {
		r.otherError = true
	}
	return err
}

func (r *recordingClient) classify(ok bool) (probeOutcome, int) {
	if ok {
		return outcomeSuccess, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unavailableCode != 0 && !r.otherError {
		return outcomeUnavailable, r.unavailableCode
	}
	return outcomeFailure, 0
}

// callProbe runs a probe function and converts a panic into a failed probe.
// This is required because probes run in their own goroutines, where an
// unrecovered panic would terminate the whole exporter.
func callProbe(p probeDetailedFunc, c fortiHTTP.FortiHTTP, meta *TargetMetadata) (m []prometheus.Metric, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Error: probe %q panicked: %v", p.name, r)
			m, ok = nil, false
		}
	}()
	return p.function(c, meta)
}

func runProbe(p probeDetailedFunc, c fortiHTTP.FortiHTTP, meta *TargetMetadata) probeResult {
	rc := newRecordingClient(c, p.unavailableCodes)
	start := time.Now()
	m, ok := callProbe(p, rc, meta)
	outcome, code := rc.classify(ok)
	res := probeResult{outcome: outcome, duration: time.Since(start), statusCode: code}
	if outcome != outcomeUnavailable {
		// On failure partial metrics are kept, as before.
		res.metrics = m
	}
	return res
}

// unavailableCache remembers probes that are not available on a target so
// they are not queried (and logged) on every scrape.
type unavailableCache struct {
	mu    sync.Mutex
	until map[string]time.Time
	now   func() time.Time
}

func newUnavailableCache() *unavailableCache {
	return &unavailableCache{until: map[string]time.Time{}, now: time.Now}
}

func (u *unavailableCache) key(target, probe string) string {
	return target + "\x00" + probe
}

func (u *unavailableCache) skip(target, probe string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	k := u.key(target, probe)
	t, ok := u.until[k]
	if !ok {
		return false
	}
	if u.now().Before(t) {
		return true
	}
	delete(u.until, k)
	return false
}

func (u *unavailableCache) mark(target, probe string, d time.Duration) {
	if d <= 0 {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.until[u.key(target, probe)] = u.now().Add(d)
}

var probeUnavailableCache = newUnavailableCache()

var (
	mProbeSuccess = prometheus.NewDesc(
		"fortigate_exporter_probe_success",
		"Whether the individual probe succeeded (1) or failed (0). Not reported for unavailable probes.",
		[]string{"probe"}, nil,
	)
	mProbeUnavailable = prometheus.NewDesc(
		"fortigate_exporter_probe_unavailable",
		"Whether the probe's API endpoint is not available on the target (1), e.g. HTTP 404 or the feature is not configured.",
		[]string{"probe"}, nil,
	)
	mProbeDuration = prometheus.NewDesc(
		"fortigate_exporter_probe_duration_seconds",
		"How long the individual probe took. Not reported for probes skipped as unavailable.",
		[]string{"probe"}, nil,
	)
)

// runProbes executes the given probes with at most `concurrency` probes in
// flight and returns the results in the order of the input slice.
func runProbes(probes []probeDetailedFunc, c fortiHTTP.FortiHTTP, meta *TargetMetadata, target string, concurrency int, recheck time.Duration, cache *unavailableCache) []probeResult {
	results := make([]probeResult, len(probes))
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(probes) {
		concurrency = len(probes)
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			for i := range jobs {
				p := probes[i]
				if cache.skip(target, p.name) {
					results[i] = probeResult{outcome: outcomeUnavailable, cached: true}
					continue
				}
				res := runProbe(p, c, meta)
				switch res.outcome {
				case outcomeUnavailable:
					cache.mark(target, p.name, recheck)
					if recheck > 0 {
						log.Printf("Probe %q is not available on %s (HTTP %d), skipping it for %s", p.name, target, res.statusCode, recheck)
					}
				case outcomeFailure:
					log.Printf("Probe %q failed on %s after %.3f seconds", p.name, target, res.duration.Seconds())
				}
				results[i] = res
			}
		})
	}
	for i := range probes {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

func (p *Collector) Probe(ctx context.Context, target map[string]string, hc *http.Client, savedConfig config.FortiExporterConfig) (bool, error) {
	tgt, err := url.Parse(target["target"])
	if err != nil {
		return false, fmt.Errorf("url.Parse failed: %v", err)
	}

	if tgt.Scheme != "https" && tgt.Scheme != "http" {
		return false, fmt.Errorf("unsupported scheme %q", tgt.Scheme)
	}

	// Filter anything else than scheme and hostname
	u := url.URL{
		Scheme: tgt.Scheme,
		Host:   tgt.Host,
	}

	if target["token"] != "" && savedConfig.AuthKeys[config.Target(target["target"])].Token == "" {
		// Add the target and its apikey to the savedConfig and use, if exists, a target entry as a template for include/exclude
		// This will only happened the "first" time
		savedConfig.AuthKeys[config.Target(target["target"])] = config.TargetAuth{
			Token:  config.Token(target["token"]),
			Probes: savedConfig.AuthKeys[config.Target(target["profile"])].Probes,
		}
	}

	c, err := fortiHTTP.NewFortiClient(ctx, u, hc, savedConfig)
	if err != nil {
		return false, err
	}

	type systemStatus struct {
		Status  string
		Version string
	}
	var st systemStatus

	// Test client connection before we blast all the probes.
	// The "system status" group has access group "any" so it is a good source
	// to test the authentication as well as fetching the OS version.
	if err := c.Get("api/v2/monitor/system/status", "", &st); err != nil {
		log.Printf("Error: API connectivity test failed, %v", err)
		return false, nil
	}

	if st.Status != "success" {
		log.Printf("Error: API connectivity test returned status: %s", st.Status)
		return false, nil
	}

	major, minor, ok := fortiversion.ParseVersion(st.Version)
	if !ok {
		log.Printf("Error: Failed to parse OS version: %q", st.Version)
		return false, nil
	}

	meta := &TargetMetadata{
		VersionMajor: major,
		VersionMinor: minor,
	}

	auth := savedConfig.AuthKeys[config.Target(u.String())]
	probes := selectProbes(availableProbes(), auth.Probes.Include, auth.Probes.Exclude)
	results := runProbes(probes, c, meta, u.String(), savedConfig.MaxConcurrency, savedConfig.UnavailableRecheck, probeUnavailableCache)

	success := true
	for i, res := range results {
		name := probes[i].name
		p.metrics = append(p.metrics, res.metrics...)

		unavailable := 0.0
		if res.outcome == outcomeUnavailable {
			unavailable = 1
		}
		p.metrics = append(p.metrics, prometheus.MustNewConstMetric(mProbeUnavailable, prometheus.GaugeValue, unavailable, name))
		if res.outcome == outcomeUnavailable {
			continue
		}

		ok := 1.0
		if res.outcome == outcomeFailure {
			ok = 0
			success = false
		}
		p.metrics = append(p.metrics,
			prometheus.MustNewConstMetric(mProbeSuccess, prometheus.GaugeValue, ok, name),
			prometheus.MustNewConstMetric(mProbeDuration, prometheus.GaugeValue, res.duration.Seconds(), name),
		)
	}

	return success, nil
}

func (p *Collector) Collect(c chan<- prometheus.Metric) {
	// Collect result of new probe functions
	for _, m := range p.metrics {
		c <- m
	}
}

func (p *Collector) Describe(_ chan<- *prometheus.Desc) {
}
