# fortigate_exporter

[![CI](https://github.com/xozy22/fortigate_exporter/actions/workflows/ci.yml/badge.svg)](https://github.com/xozy22/fortigate_exporter/actions/workflows/ci.yml)
[![GHCR release](https://github.com/xozy22/fortigate_exporter/actions/workflows/ghcr-release.yml/badge.svg)](https://github.com/xozy22/fortigate_exporter/actions/workflows/ghcr-release.yml)
[![Container image](https://img.shields.io/badge/ghcr.io-xozy22%2Ffortigate__exporter-blue?logo=docker)](https://github.com/xozy22/fortigate_exporter/pkgs/container/fortigate_exporter)

Prometheus exporter for FortiGate® firewalls, using the FortiOS REST API.

This repository is a fork of
[prometheus-community/fortigate_exporter](https://github.com/prometheus-community/fortigate_exporter)
with a ready-to-use container image, faster and more robust scrapes and
Grafana dashboards. It is tested with FortiOS 7.6.8.

> **Note:** This is not an official Fortinet product. It is developed fully
> independently by professionals and hobbyists alike.

## Contents

- [What is different in this fork](#what-is-different-in-this-fork)
- [Quick start with Docker](#quick-start-with-docker)
- [Configuration](#configuration)
  - [Key file](#key-file)
  - [Selecting probes](#selecting-probes)
  - [Dynamic configuration](#dynamic-configuration)
  - [CLI parameters](#cli-parameters)
- [FortiGate setup](#fortigate-setup)
- [Prometheus setup](#prometheus-setup)
- [Probe health](#probe-health)
- [Metrics](#metrics)
- [Grafana dashboards](#grafana-dashboards)
- [Building and releases](#building-and-releases)
- [Known issues](#known-issues)
- [Upstream, license and legal](#upstream-license-and-legal)

## What is different in this fork

- **Container image on GHCR**: `ghcr.io/xozy22/fortigate_exporter`
  (`linux/amd64`, `linux/arm64`), built on every push to `main`.
- **Parallel probes**: a scrape takes about as long as the slowest probe
  instead of the sum of all probes (`-max-concurrency`).
- **Unavailable instead of failed**: endpoints a FortiGate does not offer
  (no hardware sensors, BGP/OSPF not configured, ...) no longer set
  `probe_success` to `0`. They are skipped for an hour instead of being
  queried and logged on every scrape.
- **Per-probe metrics**: success, availability and duration of every probe.
- **No more empty scrapes**: duplicate series (e.g. two Wi-Fi APs with the
  same name, IPv6 DNS servers) and panicking probes no longer turn the whole
  scrape into an HTTP 500.
- **Grafana dashboards** in [`grafana-dashboards/`](grafana-dashboards/).

See the [CHANGELOG](CHANGELOG.md) for details.

## Quick start with Docker

**1. Create an API user** on the FortiGate and note its token, see
[FortiGate setup](#fortigate-setup).

**2. Create `fortigate-key.yaml`:**

```yaml
"https://192.168.1.99":
  token: your-api-token
```

**3. Run the container** straight from this repository's registry:

```bash
docker run -d --name fortigate_exporter --restart unless-stopped \
  -p 9710:9710 \
  -v "$(pwd)/fortigate-key.yaml:/config/fortigate-key.yaml:ro" \
  ghcr.io/xozy22/fortigate_exporter:main
```

The image reads `/config/fortigate-key.yaml` by default. If you pass your own
arguments after the image name, they replace the defaults, so keep
`-auth-file /config/fortigate-key.yaml`:

```bash
docker run ... ghcr.io/xozy22/fortigate_exporter:main \
  -auth-file /config/fortigate-key.yaml -insecure
```

Or with Docker Compose (see [`docker-compose.yml`](docker-compose.yml)):

```yaml
services:
  fortigate_exporter:
    image: ghcr.io/xozy22/fortigate_exporter:main
    ports:
      - "9710:9710"
    volumes:
      - ./fortigate-key.yaml:/config/fortigate-key.yaml:ro
    restart: unless-stopped
```

**4. Test it:**

```bash
curl 'http://localhost:9710/probe?target=https://192.168.1.99'
```

You should see `probe_success 1` and lots of `fortigate_*` metrics. The
exporter's own metrics are available at `http://localhost:9710/metrics`.

### Image tags

| Tag | Content |
|---|---|
| `main` | Latest build of the `main` branch |
| `X.Y.Z`, `X.Y` | Tagged release |
| `latest` | Most recent tagged release |

The image is based on `distroless/static` and runs as a non-root user, so the
key file must be readable for others (e.g. `chmod 644 fortigate-key.yaml`) or
for UID 65532.

## Configuration

### Key file

The key file maps FortiGate targets to API tokens. A target is the scheme,
host and optional port, exactly as it is passed in the `target` parameter:

```yaml
"https://my-fortigate":
  token: api-key-goes-here

"https://my-other-fortigate:8443":
  token: api-key-goes-here
  probes:
    exclude:
      - System/SensorInfo
```

Only token authentication is supported. FortiOS accepts tokens only over
HTTPS, and the exporter sends them in the `Authorization: Bearer` header, so
it works with FortiOS 7.4.5+ / 7.6.1+ where tokens in URL parameters are
disabled by default.

### Selecting probes

Probes can be selected per target with optional `include` and `exclude` lists:

- Entries are **case-sensitive prefixes** of probe names, e.g. `System`
  matches all `System/...` probes.
- `include` is evaluated first, then `exclude` removes probes again.
- No `probes` section or an empty `include` list means all probes.
- `- ''` in `include` includes all probes; `- ''` in `exclude` excludes all.

```yaml
"https://my-fortigate":
  token: api-key-goes-here
  probes:
    include:
      - System
      - VPN
      - Firewall/Policies
"https://my-other-fortigate:8443":
  token: api-key-goes-here
  probes:
    include:
      - System
      - Firewall
    exclude:
      - System/LinkMonitor
```

Probe names are listed in the [permission table](#fortigate-setup).

### Dynamic configuration

If Prometheus discovers the FortiGates itself, the token can be passed as a
`token` query parameter instead of being listed in the key file:

```bash
curl 'localhost:9710/probe?target=https://192.168.2.31&token=ghi6eItWzWewgbrFMsazvBVwDjZzzb'
```

An additional `profile` parameter uses the `probes` section of a key file
entry with that name:

```bash
curl 'localhost:9710/probe?target=https://192.168.2.31&token=...&profile=fs124e'
```

```yaml
fs124e:
  # token: not used
  probes:
    include:
      - System
      - Firewall
```

### CLI parameters

| Flag | Default | Description |
|---|---|---|
| `-auth-file` | `fortigate-key.yaml` (image: `/config/fortigate-key.yaml`) | Path to the key file |
| `-listen` | `:9710` | Address to listen on |
| `-scrape-timeout` | `30` | Maximum duration of a scrape in seconds |
| `-https-timeout` | `10` | TLS handshake timeout in seconds |
| `-insecure` | off | Do not verify the FortiGate's TLS certificate |
| `-extra-ca-certs` | none | Comma-separated PEM files to trust in addition to the system store |
| `-max-concurrency` | `4` | Probes running in parallel per target (`1` = sequential) |
| `-unavailable-recheck-interval` | `1h` | How long unavailable probes are skipped before they are tried again (`0` = every scrape) |
| `-max-bgp-paths` | `10000` | Maximum number of BGP paths to fetch per IP version |
| `-max-vpn-users` | `0` | Maximum number of VPN users to fetch (`0` = none) |

## FortiGate setup

Create a REST API administrator with a read-only admin profile and restrict
its trusted hosts to the exporter.

```
config system accprofile
    edit "monitor"
        # global scope fails on firewalls without multi-VDOM
        set scope global
        set authgrp read
        # since FortiOS 6.2.1 use 'fwgrp read' for load balancer metrics
        set fwgrp custom
        set loggrp custom
        set netgrp custom
        set sysgrp custom
        set vpngrp read
        set wifi read
        config fwgrp-permission
            set policy read
            set others read
        end
        config netgrp-permission
            set cfg read
            set route-cfg read
        end
        config loggrp-permission
            set config read
        end
        config sysgrp-permission
            set cfg read
        end
    next
end
```

Then create the API user under **System → Administrators → Create New →
REST API Admin** with this profile and the exporter's IP as trusted host, and
copy the generated token into the key file.

Read permission is enough. Grant only what the probes you use need:

| Probe | Permission | API endpoint |
|---|---|---|
| *Connectivity test* | *any* | api/v2/monitor/system/status |
| BGP/NeighborPaths/IPv4 | netgrp.route-cfg | api/v2/monitor/router/bgp/paths |
| BGP/NeighborPaths/IPv6 | netgrp.route-cfg | api/v2/monitor/router/bgp/paths6 |
| BGP/Neighbors/IPv4 | netgrp.route-cfg | api/v2/monitor/router/bgp/neighbors |
| BGP/Neighbors/IPv6 | netgrp.route-cfg | api/v2/monitor/router/bgp/neighbors6 |
| Firewall/IPPool | fwgrp.policy | api/v2/monitor/firewall/ippool |
| Firewall/LoadBalance | fwgrp.others | api/v2/monitor/firewall/load-balance<br>api/v2/cmdb/firewall/vip |
| Firewall/Policies | fwgrp.policy | api/v2/monitor/firewall/policy/select<br>api/v2/monitor/firewall/policy6/select<br>api/v2/cmdb/firewall/policy<br>api/v2/cmdb/firewall/policy6 |
| License/Status | *any* | api/v2/monitor/license/status/select |
| Log/Fortianalyzer/Status | loggrp.config | api/v2/monitor/log/fortianalyzer |
| Log/Fortianalyzer/Queue | loggrp.config | api/v2/monitor/log/fortianalyzer-queue |
| Log/DiskUsage | loggrp.config | api/v2/monitor/log/current-disk-usage |
| Network/Dns/Latency | sysgrp.cfg | api/v2/monitor/network/dns/latency |
| OSPF/Neighbors | netgrp.route-cfg | api/v2/monitor/router/ospf/neighbors |
| Switch/ManagedSwitch | switch | api/v2/monitor/switch-controller/managed-switch |
| System/AvailableCertificates | *any* | api/v2/monitor/system/available-certificates |
| System/Central-Management/Status | sysgrp.cfg | api/v2/monitor/system/central-management/status |
| System/Fortimanager/Status | sysgrp.cfg | api/v2/monitor/system/fortimanager/status |
| System/Global/Location | sysgrp.cfg | api/v2/cmdb/system/global |
| System/HAChecksum | sysgrp.cfg | api/v2/monitor/system/ha-checksums |
| System/HAStatistics | sysgrp.cfg | api/v2/monitor/system/ha-statistics<br>api/v2/cmdb/system/ha |
| System/Ha-peer | sysgrp.cfg | api/v2/monitor/system/ha-peer |
| System/Interface | netgrp.cfg | api/v2/monitor/system/interface/select |
| System/Interface/Transceivers | *any* | api/v2/monitor/system/interface/transceivers |
| System/LinkMonitor | sysgrp.cfg | api/v2/monitor/system/link-monitor |
| System/Ntp/Status | netgrp.cfg | api/v2/monitor/system/ntp/status |
| System/Performance/Status | sysgrp.cfg | api/v2/monitor/system/performance/status |
| System/Resource/Usage | sysgrp.cfg | api/v2/monitor/system/resource/usage |
| System/Resource/Usage/VDOM | sysgrp.cfg | api/v2/monitor/system/resource/usage |
| System/SDNConnector | sysgrp.cfg | api/v2/monitor/system/sdn-connector/status |
| System/SensorInfo | sysgrp.cfg | api/v2/monitor/system/sensor-info |
| System/Status | *any* | api/v2/monitor/system/status |
| System/Time/Clock | sysgrp.cfg | api/v2/monitor/system/time |
| System/VDOMResource | sysgrp.cfg | api/v2/monitor/system/vdom-resource |
| User/Fsso | authgrp | api/v2/monitor/user/fsso |
| VPN/IPSec | vpngrp | api/v2/monitor/vpn/ipsec |
| VPN/Ssl/Connections | vpngrp | api/v2/monitor/vpn/ssl |
| VPN/Ssl/Stats | vpngrp | api/v2/monitor/vpn/ssl/stats |
| VirtualWAN/HealthCheck | netgrp.cfg | api/v2/monitor/virtual-wan/health-check |
| WebUI/State | *any* | api/v2/monitor/web-ui/state |
| Wifi/APStatus | wifi | api/v2/monitor/wifi/ap_status |
| Wifi/Clients | wifi | api/v2/monitor/wifi/client |
| Wifi/ManagedAP | wifi | api/v2/monitor/wifi/managed_ap |

A missing permission results in HTTP 403 for that probe: it is reported as
failed and logged, all other probes keep working. Exclude probes you do not
need instead of granting their permission.

## Prometheus setup

```yaml
scrape_configs:
  - job_name: fortigate_exporter
    metrics_path: /probe
    scrape_interval: 60s
    scrape_timeout: 30s
    static_configs:
      - targets:
          - https://my-fortigate
          - https://my-other-fortigate:8443
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
        # drop https:// and the port for the instance label
        regex: '(?:.+)(?::\/\/)([^:]*).*'
      - target_label: __address__
        replacement: 'fortigate-exporter-host:9710'
```

A scrape takes as long as the slowest probe, typically a few seconds (the
FortiAnalyzer status endpoint alone can take several seconds on the
FortiGate). Prometheus' default `scrape_timeout` of 10 s is tight; 30 s is a
safe value. Check the actual duration with `probe_duration_seconds`.

With [dynamic configuration](#dynamic-configuration):

```yaml
  - job_name: fortigate_exporter
    metrics_path: /probe
    file_sd_configs:
      - files:
          - /etc/prometheus/file_sd/fws/*.yml
    params:
      profile:
        - fs124e
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [token]
        target_label: __param_token
      - source_labels: [__param_target]
        regex: '(?:.+)(?::\/\/)([^:]*).*'
        target_label: instance
      - target_label: __address__
        replacement: 'fortigate-exporter-host:9710'
      - action: labeldrop
        regex: token
```

> The final `labeldrop` keeps the token out of your time series. The token is
> still visible on the Prometheus targets page, so protect the Prometheus UI
> (bind to localhost, basic auth, reverse proxy).

## Probe health

Not every FortiGate offers every API endpoint: models without hardware sensors
answer `system/sensor-info` with `404`, and FortiOS answers the BGP and OSPF
endpoints with `500` when the protocol is not configured. Such probes are
reported as **unavailable**, logged once and skipped for
`-unavailable-recheck-interval`.

| HTTP status | Treated as unavailable for |
|---|---|
| 404 Not Found, 424 Failed Dependency | all probes |
| 500 Internal Server Error | `BGP/*`, `OSPF/*` |

Every other error marks the probe as failed and sets `probe_success` to `0`.

| Metric | Description |
|---|---|
| `probe_success` | `1` if no probe failed |
| `probe_duration_seconds` | Duration of the whole scrape |
| `fortigate_exporter_probe_success{probe}` | `1` = succeeded, `0` = failed (not reported for unavailable probes) |
| `fortigate_exporter_probe_unavailable{probe}` | `1` if the endpoint is not available on the target |
| `fortigate_exporter_probe_duration_seconds{probe}` | Duration of the probe |

```promql
# failing probes
fortigate_exporter_probe_success == 0
# slowest probes
topk(5, fortigate_exporter_probe_duration_seconds)
```

## Metrics

All metrics are listed in [metrics.md](metrics.md), PromQL examples are in
[EXAMPLES.md](EXAMPLES.md).

## Grafana dashboards

[`grafana-dashboards/`](grafana-dashboards/) contains two ready-to-import
dashboards (Grafana 12, dashboard schema v2):

- **`fortigate-modern.json`**: KPI strip on top, details in tabs (overview,
  WAN & SD-WAN, network, policies, VPN, Wi-Fi, switching, system, exporter)
- **`fortigate-classic.json`**: classic row-based layout

Import via **Dashboards → New → Import** and select your Prometheus data
source. Details are in the [dashboard README](grafana-dashboards/README.md).

## Building and releases

Build the binary (Go version from `go.mod`):

```bash
go build -o fortigate_exporter .
./fortigate_exporter -auth-file fortigate-key.yaml
```

Build the container image locally:

```bash
docker build -f Dockerfile.ghcr -t fortigate_exporter .
```

The [`ghcr-release.yml`](.github/workflows/ghcr-release.yml) workflow runs the
tests and publishes the image on every push to `main`. Pushing a tag like
`v1.26.0` additionally publishes the versioned image and a GitHub release with
binaries for Linux (amd64, arm64, arm), Windows and macOS.

## Known issues

- Probing can cause an
  [httpsd memory leak in FortiOS 6.2.x](https://github.com/prometheus-community/fortigate_exporter/issues/62)
  ([workaround](https://github.com/prometheus-community/fortigate_exporter/issues/62#issuecomment-798602061)).
  On such systems use `-max-concurrency 1`.
- IPsec phase 2 metrics (`fortigate_ipsec_tunnel_*`) only exist while the
  FortiGate reports phase 2 selectors. Phase 1 counters
  (`fortigate_ipsec_tunnel_parent_*`) are always available.

Missing a metric or found a bug? Please
[open an issue](https://github.com/xozy22/fortigate_exporter/issues/new).
As an alternative to this exporter, FortiGates can be polled via SNMP, e.g.
with the [Prometheus SNMP exporter](https://github.com/prometheus/snmp_exporter),
within the [limits of FortiOS' SNMP support](https://kb.fortinet.com/kb/documentLink.do?externalID=FD47703).

## Upstream, license and legal

This fork is based on
[prometheus-community/fortigate_exporter](https://github.com/prometheus-community/fortigate_exporter),
originally created by Christian Svensson. Licensed under the
[Apache License 2.0](LICENSE).

Fortinet® and FortiGate® are registered trademarks of Fortinet, Inc. This is
not an official Fortinet product.
