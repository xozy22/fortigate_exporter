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

package probe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/prometheus-community/fortigate_exporter/internal/config"
	fortiHTTP "github.com/prometheus-community/fortigate_exporter/pkg/fortigatehttpclient"
)

// errClient answers every request with a fixed error.
type errClient struct{ err error }

func (e errClient) Get(string, string, any) error { return e.err }

func statusErr(code int) error {
	return &fortiHTTP.HTTPStatusError{StatusCode: code, Path: "api/v2/test"}
}

var dummyMetricDesc = prometheus.NewDesc("fortigate_test_metric", "test", nil, nil)

func getProbe(path string) probeFunc {
	return func(c fortiHTTP.FortiHTTP, _ *TargetMetadata) ([]prometheus.Metric, bool) {
		var v any
		if err := c.Get(path, "", &v); err != nil {
			return nil, false
		}
		return []prometheus.Metric{prometheus.MustNewConstMetric(dummyMetricDesc, prometheus.GaugeValue, 1)}, true
	}
}

func TestRunProbeClassification(t *testing.T) {
	meta := &TargetMetadata{VersionMajor: 7, VersionMinor: 6}
	tests := []struct {
		name  string
		err   error
		codes []int
		want  probeOutcome
		code  int
	}{
		{"success", nil, nil, outcomeSuccess, 0},
		{"404 is unavailable", statusErr(404), nil, outcomeUnavailable, 404},
		{"424 is unavailable", statusErr(424), nil, outcomeUnavailable, 424},
		{"500 is a failure by default", statusErr(500), nil, outcomeFailure, 0},
		{"500 is unavailable for routing probes", statusErr(500), routingUnavailableCodes, outcomeUnavailable, 500},
		{"403 is a failure", statusErr(403), nil, outcomeFailure, 0},
		{"wrapped 404 is unavailable", fmt.Errorf("wrapped: %w", statusErr(404)), nil, outcomeUnavailable, 404},
		{"other error is a failure", errors.New("json: cannot unmarshal"), nil, outcomeFailure, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := probeDetailedFunc{name: "Test", function: getProbe("api/v2/test"), unavailableCodes: tc.codes}
			res := runProbe(p, errClient{err: tc.err}, meta)
			if res.outcome != tc.want {
				t.Fatalf("outcome = %v, want %v", res.outcome, tc.want)
			}
			if res.statusCode != tc.code {
				t.Errorf("statusCode = %d, want %d", res.statusCode, tc.code)
			}
			if tc.want == outcomeSuccess && len(res.metrics) != 1 {
				t.Errorf("expected 1 metric on success, got %d", len(res.metrics))
			}
		})
	}
}

// mixedClient returns 404 for one path and another error for the rest.
type mixedClient struct{}

func (mixedClient) Get(path, _ string, _ any) error {
	if path == "a" {
		return statusErr(404)
	}
	return statusErr(403)
}

func TestRunProbeMixedErrorsIsFailure(t *testing.T) {
	p := probeDetailedFunc{name: "Mixed", function: func(c fortiHTTP.FortiHTTP, _ *TargetMetadata) ([]prometheus.Metric, bool) {
		var v any
		e1 := c.Get("a", "", &v)
		e2 := c.Get("b", "", &v)
		return nil, e1 == nil && e2 == nil
	}}
	if res := runProbe(p, mixedClient{}, &TargetMetadata{}); res.outcome != outcomeFailure {
		t.Fatalf("outcome = %v, want failure", res.outcome)
	}
}

func TestRunProbeRecoversPanic(t *testing.T) {
	p := probeDetailedFunc{name: "Panics", function: func(fortiHTTP.FortiHTTP, *TargetMetadata) ([]prometheus.Metric, bool) {
		var s []int
		_ = s[0] // index out of range
		return nil, true
	}}
	res := runProbe(p, errClient{}, &TargetMetadata{})
	if res.outcome != outcomeFailure {
		t.Fatalf("outcome = %v, want failure", res.outcome)
	}
}

func TestRunProbesOrderAndConcurrency(t *testing.T) {
	var inFlight, maxInFlight int32
	const n = 20
	probes := make([]probeDetailedFunc, n)
	for i := range n {
		desc := prometheus.NewDesc(fmt.Sprintf("fortigate_test_%d", i), "test", nil, nil)
		probes[i] = probeDetailedFunc{name: fmt.Sprintf("P%d", i), function: func(fortiHTTP.FortiHTTP, *TargetMetadata) ([]prometheus.Metric, bool) {
			cur := atomic.AddInt32(&inFlight, 1)
			for {
				old := atomic.LoadInt32(&maxInFlight)
				if cur <= old || atomic.CompareAndSwapInt32(&maxInFlight, old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&inFlight, -1)
			return []prometheus.Metric{prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, 1)}, true
		}}
	}

	start := time.Now()
	results := runProbes(probes, errClient{}, &TargetMetadata{}, "https://t", 4, time.Hour, newUnavailableCache())
	elapsed := time.Since(start)

	if got := atomic.LoadInt32(&maxInFlight); got > 4 {
		t.Errorf("max concurrent probes = %d, want <= 4", got)
	}
	if got := atomic.LoadInt32(&maxInFlight); got < 2 {
		t.Errorf("probes did not run in parallel (max in flight %d)", got)
	}
	// 20 probes * 10ms sequential would be 200ms; with 4 workers ~50ms.
	if elapsed > 180*time.Millisecond {
		t.Errorf("parallel run took %s, expected well below sequential time", elapsed)
	}
	for i, r := range results {
		if r.outcome != outcomeSuccess || len(r.metrics) != 1 {
			t.Fatalf("result %d: outcome %v, %d metrics", i, r.outcome, len(r.metrics))
		}
		want := fmt.Sprintf("fortigate_test_%d", i)
		if !strings.Contains(r.metrics[0].Desc().String(), want) {
			t.Errorf("result %d out of order: %s", i, r.metrics[0].Desc())
		}
	}
}

func TestUnavailableCache(t *testing.T) {
	now := time.Unix(1000, 0)
	c := newUnavailableCache()
	c.now = func() time.Time { return now }

	if c.skip("t", "p") {
		t.Fatal("empty cache must not skip")
	}
	c.mark("t", "p", time.Minute)
	if !c.skip("t", "p") {
		t.Fatal("marked probe must be skipped")
	}
	if c.skip("t2", "p") || c.skip("t", "p2") {
		t.Fatal("cache must be keyed by target and probe")
	}
	now = now.Add(2 * time.Minute)
	if c.skip("t", "p") {
		t.Fatal("expired entry must not be skipped")
	}
	c.mark("t", "p", 0)
	if c.skip("t", "p") {
		t.Fatal("recheck interval 0 must disable caching")
	}
}

func TestSelectProbes(t *testing.T) {
	all := availableProbes()
	names := func(ps []probeDetailedFunc) []string {
		r := []string{}
		for _, p := range ps {
			r = append(r, p.name)
		}
		return r
	}
	if got := selectProbes(all, nil, nil); len(got) != len(all) {
		t.Errorf("no filters: got %d probes, want %d", len(got), len(all))
	}
	got := names(selectProbes(all, config.ProbeList{"BGP"}, config.ProbeList{"BGP/NeighborPaths"}))
	want := []string{"BGP/Neighbors/IPv4", "BGP/Neighbors/IPv6"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("include/exclude: got %v, want %v", got, want)
	}
	for _, p := range all {
		if strings.HasPrefix(p.name, "BGP/") || strings.HasPrefix(p.name, "OSPF/") {
			if len(p.unavailableCodes) == 0 {
				t.Errorf("routing probe %q should treat HTTP 500 as unavailable", p.name)
			}
		}
	}
}

// TestProbeEndToEnd runs the full Probe() against a fake FortiGate that has
// no BGP configured, no hardware sensors and denies access to IP pools.
func TestProbeEndToEnd(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/v2/monitor/system/status":
			fmt.Fprint(w, `{"status":"success","version":"v7.6.8"}`)
		case strings.HasPrefix(r.URL.Path, "/api/v2/monitor/router/"):
			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/api/v2/monitor/system/sensor-info":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()

	newCfg := func(exclude config.ProbeList) config.FortiExporterConfig {
		return config.FortiExporterConfig{
			MaxConcurrency:     4,
			UnavailableRecheck: time.Hour,
			AuthKeys: config.AuthKeys{
				config.Target(srv.URL): {
					Token: "secret",
					Probes: config.Probes{
						Include: config.ProbeList{"BGP/Neighbors/IPv4", "System/SensorInfo", "Firewall/IPPool"},
						Exclude: exclude,
					},
				},
			},
		}
	}

	gather := func(cfg config.FortiExporterConfig) (bool, map[string]float64) {
		pc := &Collector{}
		ok, err := pc.Probe(context.Background(), map[string]string{"target": srv.URL}, srv.Client(), cfg)
		if err != nil {
			t.Fatalf("Probe returned error: %v", err)
		}
		reg := prometheus.NewRegistry()
		reg.MustRegister(pc)
		mfs, err := reg.Gather()
		if err != nil {
			t.Fatalf("gather: %v", err)
		}
		vals := map[string]float64{}
		for _, mf := range mfs {
			for _, m := range mf.GetMetric() {
				vals[mf.GetName()+"{"+labelValue(m, "probe")+"}"] = m.GetGauge().GetValue()
			}
		}
		return ok, vals
	}

	ok, vals := gather(newCfg(nil))
	if ok {
		t.Error("probe_success must be false while Firewall/IPPool fails with 403")
	}
	expect := map[string]float64{
		"fortigate_exporter_probe_unavailable{BGP/Neighbors/IPv4}": 1,
		"fortigate_exporter_probe_unavailable{System/SensorInfo}":  1,
		"fortigate_exporter_probe_unavailable{Firewall/IPPool}":    0,
		"fortigate_exporter_probe_success{Firewall/IPPool}":        0,
	}
	for k, v := range expect {
		got, found := vals[k]
		if !found || got != v {
			t.Errorf("%s = %v (found %v), want %v", k, got, found, v)
		}
	}
	if _, found := vals["fortigate_exporter_probe_success{BGP/Neighbors/IPv4}"]; found {
		t.Error("unavailable probes must not report probe_success")
	}

	// Without the failing probe the scrape is successful, and the unavailable
	// probes are served from the cache without hitting the FortiGate again.
	mu.Lock()
	sensorHits := hits["/api/v2/monitor/system/sensor-info"]
	mu.Unlock()
	ok, vals = gather(newCfg(config.ProbeList{"Firewall/IPPool"}))
	if !ok {
		t.Error("probe_success must be true when only unavailable probes are left")
	}
	if vals["fortigate_exporter_probe_unavailable{System/SensorInfo}"] != 1 {
		t.Error("cached unavailable probe must still be reported as unavailable")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits["/api/v2/monitor/system/sensor-info"] != sensorHits {
		t.Errorf("sensor-info was queried again although it is cached as unavailable")
	}
}

func labelValue(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}
