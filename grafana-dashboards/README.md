# Grafana dashboards

Two dashboards for this exporter, both in the Grafana dashboard **schema v2**
format (Grafana 12 with the dynamic dashboards feature).

| File | Layout | Language |
|---|---|---|
| [`fortigate-modern.json`](fortigate-modern.json) | KPI strip on top, details in tabs: Übersicht, WAN & SD-WAN, Netzwerk, Policies, VPN, WLAN, Switching, System, Exporter | German |
| [`fortigate-classic.json`](fortigate-classic.json) | Classic rows: System, HA, Interface, Logs, FortiWifi, FortiSwitch, Policies, VPN, SD-WAN, Certificate, Exporter Health | English |

Both dashboards use separate UIDs, so they can be imported side by side.

## Import

1. Grafana → **Dashboards → New → Import**
2. Upload the JSON file
3. Select your Prometheus data source when asked

The `firewall` variable lists every target from `fortigate_version_info`
(`instance` label), so one dashboard covers all FortiGates scraped by the
exporter.

## Highlights

- **Exporter health**: `probe_success`, scrape duration, slowest probes,
  probe history and probes that are not available on the target. This uses
  the per-probe metrics `fortigate_exporter_probe_*` of this fork.
- **Certificates** as a table sorted by days left (red < 14, orange < 30).
- **Policies**: top 10 by volume and hits in the selected time range, plus
  a list of policies without a single hit in the time range (set the range
  to e.g. 30 days before cleaning up).
- **Interfaces** table with link, speed, current throughput and volume.
- **Wi-Fi**: AP table with CPU/memory, clients per AP, channel utilization
  and interfering APs per active radio.
- **SD-WAN**: member status timeline, latency, jitter, packet loss and
  bandwidth; link monitor and DNS latency.
- Throughput graphs are mirrored: receive up, transmit down.
- FortiGate reboots are shown as annotations (modern dashboard).

Panels for features that are not configured (e.g. HA, BGP, IPsec phase 2
without an established tunnel) simply show no data.
