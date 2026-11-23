# api/v2/monitor/wifi/managed_ap?vdom=*
# Two managed APs with the same name but different serials, e.g. a replaced
# FortiAP that is still listed (offline) next to the new one.
[
  {
    "http_method": "GET",
    "results": [
      {
        "name": "Erdgeschoss",
        "vdom": "root",
        "serial": "FP231F0000000001",
        "ap_profile": "default",
        "os_version": "FP231F-v7.6-build0123",
        "join_time_raw": 1700000000,
        "cpu_usage": 5,
        "mem_free": 100,
        "mem_total": 200,
        "radio": [ { "radio_id": 1, "client_count": 3, "oper_chan": 6 } ],
        "wired": [ { "interface": "lan1", "bytes_rx": 10, "bytes_tx": 20 } ]
      },
      {
        "name": "Erdgeschoss",
        "vdom": "root",
        "serial": "FP231F0000000002",
        "ap_profile": "default",
        "os_version": "FP231F-v7.4-build0999",
        "join_time_raw": 0,
        "cpu_usage": 0,
        "mem_free": 0,
        "mem_total": 0,
        "radio": [ { "radio_id": 1 } ],
        "wired": []
      }
    ],
    "vdom": "root",
    "path": "wifi",
    "name": "managed_ap",
    "status": "success",
    "version": "v7.6.8",
    "build": 3744
  }
]
