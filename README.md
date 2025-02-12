# jsonping

**Note: This is a work in progress. For a fully implemented JSON output in fping, check out [this pull request](https://github.com/schweikert/fping/pull/380).**

A high-performance, low-level ICMP ping utility that outputs results in JSON format. Built with raw sockets for maximum efficiency and control.

## Features

- **Pure Go Implementation**: Uses only the Go standard library and minimal dependencies
- **Raw Socket ICMP**: Direct ICMP packet construction and handling
- **JSON Output**: Clean, structured output for easy parsing and integration
- **Live Progress**: JSON-formatted progress updates for each ping
- **Comprehensive Statistics**: Tracks min/max/avg latency, packet loss, and more
- **Configurable Parameters**: Customize ping count, interval, size, and more
- **Graceful Shutdown**: Handles interrupts cleanly
- **IPv4 Support**: Focused on IPv4 ping functionality
- **Parallel Mode**: Support for pinging multiple hosts in parallel

## Installation

1. Clone the repository
2. Build the binary:
   ```bash
   ./build.sh
   ```

The build script will:
- Download dependencies
- Compile with optimizations
- Set necessary capabilities for non-root ping

## Usage

```bash
jsonping [flags] target

Flags:
  -c, --count int      number of pings to send (default 10)
  -i, --interval int   interval between pings in milliseconds (default 1000)
  -t, --timeout int    timeout in milliseconds for each ping (default 5000)
  -s, --size int       size of ping packet payload in bytes (default 56)
  -q, --quiet          only show summary statistics
  -r, --retry int      number of retries for failed pings (default 0)
  -p, --period int     time in milliseconds between ping retries (default 1000)
  -S, --source string  source IP address
  -6, --ipv6          use IPv6 instead of IPv4
  -a, --parallel      ping hosts in parallel
  -B, --batch int     max hosts to ping in parallel (with -a) (default 100)
  -M, --dont-fragment set don't fragment bit in IP header
```

### Examples

1. Simple ping with progress updates:
```bash
$ jsonping -c 2 8.8.8.8
{"target":"8.8.8.8","sequence":1,"size":64,"latency":12.33,"avg_latency":12.33,"loss":0}
{"target":"8.8.8.8","sequence":2,"size":64,"latency":12.34,"avg_latency":12.34,"loss":0}
[
  {
    "target":"8.8.8.8",
    "transmitted":2,
    "received":2,
    "loss_percentage":0,
    "min_latency_ms":12.33,
    "avg_latency_ms":12.34,
    "max_latency_ms":12.34,
    "start_time":"2025-02-12T08:55:58+10:00",
    "end_time":"2025-02-12T08:55:59+10:00"
  }
]
```

2. Quiet mode (summary only):
```bash
$ jsonping -q -c 2 8.8.8.8
[
  {
    "target":"8.8.8.8",
    "transmitted":2,
    "received":2,
    "loss_percentage":0,
    "min_latency_ms":12.33,
    "avg_latency_ms":12.34,
    "max_latency_ms":12.34,
    "start_time":"2025-02-12T08:55:58+10:00",
    "end_time":"2025-02-12T08:55:59+10:00"
  }
]
```

3. Parallel ping to multiple hosts:
```bash
$ jsonping -a -c 2 8.8.8.8 1.1.1.1
{"target":"1.1.1.1","sequence":1,"size":64,"latency":0.46,"avg_latency":0.46,"loss":0}
{"target":"8.8.8.8","sequence":1,"size":64,"latency":12.33,"avg_latency":12.33,"loss":0}
{"target":"1.1.1.1","sequence":2,"size":64,"latency":0.60,"avg_latency":0.53,"loss":0}
{"target":"8.8.8.8","sequence":2,"size":64,"latency":12.34,"avg_latency":12.34,"loss":0}
[
  {
    "target":"1.1.1.1",
    "transmitted":2,
    "received":2,
    "loss_percentage":0,
    "min_latency_ms":0.46,
    "avg_latency_ms":0.53,
    "max_latency_ms":0.60,
    "start_time":"2025-02-12T08:55:03+10:00",
    "end_time":"2025-02-12T08:55:04+10:00"
  },
  {
    "target":"8.8.8.8",
    "transmitted":2,
    "received":2,
    "loss_percentage":0,
    "min_latency_ms":12.33,
    "avg_latency_ms":12.34,
    "max_latency_ms":12.34,
    "start_time":"2025-02-12T08:55:58+10:00",
    "end_time":"2025-02-12T08:55:59+10:00"
  }
]
```
```

3. Parallel ping to multiple hosts:
```bash
$ jsonping -a -c 3 8.8.8.8 1.1.1.1 9.9.9.9
[
  {
    "target": "8.8.8.8",
    "transmitted": 3,
    "received": 3,
    "loss_percentage": 0,
    "min_latency_ms": 15.1,
    "avg_latency_ms": 16.2,
    "max_latency_ms": 17.3,
    "start_time": "2025-02-12T07:32:51+10:00",
    "end_time": "2025-02-12T07:32:52+10:00"
  },
  {
    "target": "1.1.1.1",
    "transmitted": 3,
    "received": 3,
    "loss_percentage": 0,
    "min_latency_ms": 14.2,
    "avg_latency_ms": 15.1,
    "max_latency_ms": 16.0,
    "start_time": "2025-02-12T07:32:51+10:00",
    "end_time": "2025-02-12T07:32:52+10:00"
  },
  {
    "target": "9.9.9.9",
    "transmitted": 3,
    "received": 3,
    "loss_percentage": 0,
    "min_latency_ms": 18.4,
    "avg_latency_ms": 19.2,
    "max_latency_ms": 20.1,
    "start_time": "2025-02-12T07:32:51+10:00",
    "end_time": "2025-02-12T07:32:52+10:00"
  }
]
```

## JSON Output Format

```json
{
  "target": "string",           // Target hostname or IP
  "transmitted": int,           // Number of packets sent
  "received": int,             // Number of packets received
  "loss_percentage": float,    // Packet loss percentage
  "min_latency_ms": float,    // Minimum round-trip time in ms
  "avg_latency_ms": float,    // Average round-trip time in ms
  "max_latency_ms": float,    // Maximum round-trip time in ms
  "start_time": "string",     // ISO 8601 timestamp
  "end_time": "string",       // ISO 8601 timestamp
  "error": "string"           // Error message (if any)
}
```

## Technical Details

### ICMP Implementation
- Uses raw sockets for direct ICMP packet construction
- Implements ICMP Echo Request (Type 8) and handles Echo Reply (Type 0)
- Custom checksum calculation
- Proper packet sequence numbering
- Process ID-based packet identification

### Performance Considerations
- Zero-allocation packet handling where possible
- Efficient binary encoding/decoding
- Minimal memory footprint
- Non-blocking I/O with deadlines

## Requirements

- Linux operating system
- CAP_NET_RAW capability (set automatically by build script)
- Go 1.16 or later

## Security

The program requires raw socket capabilities to send ICMP packets. The build script automatically sets the necessary capability (cap_net_raw) on the binary, allowing non-root users to send pings.

## License

MIT License
