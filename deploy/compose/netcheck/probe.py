"""Connection probe (REQ-OPS-001). Usage: probe.py host:port [host:port ...]. Prints JSON.

A target is "open" when a TCP connection succeeds within two seconds, otherwise "refused"
(the name does not resolve, the network is not shared, or nothing listens).
"""
import json
import socket
import sys

result = {}
for target in sys.argv[1:]:
    host, port = target.rsplit(":", 1)
    try:
        socket.create_connection((host, int(port)), 2).close()
        result[target] = "open"
    except OSError:
        result[target] = "refused"
print(json.dumps(result))
