# api/v2/monitor/network/dns/latency with IPv6 DNS servers (see #393):
# FortiOS reports the same "ip" for both IPv6 servers, only "ip6" differs.
{
    "http_method": "GET",
    "results": [
        { "service": "dns_server", "latency": 10, "last_update": 1040, "ip": "9.9.9.9" },
        { "service": "dns_server", "latency": 12, "last_update": 17180, "ip": "32.1.22.32", "ip6": "2001:1620:2777:2::20" },
        { "service": "dns_server", "latency": 14, "last_update": 46910, "ip": "32.1.22.32", "ip6": "2001:1620:2777:1::10" }
    ],
    "vdom": "root",
    "path": "network",
    "name": "dns",
    "action": "latency",
    "status": "success",
    "version": "v7.6.8",
    "build": 3744
}
