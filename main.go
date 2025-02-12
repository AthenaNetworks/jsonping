package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

type PingResult struct {
	Target         string    `json:"target"`
	Transmitted    int       `json:"transmitted"`
	Received       int       `json:"received"`
	LossPercentage float64   `json:"loss_percentage"`
	MinLatency     float64   `json:"min_latency_ms"`
	AvgLatency     float64   `json:"avg_latency_ms"`
	MaxLatency     float64   `json:"max_latency_ms"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	Error          string    `json:"error,omitempty"`
	MTU            int       `json:"mtu,omitempty"`
}

// Progress represents a single ping result for progress output
type Progress struct {
	Target     string  `json:"target"`
	Sequence   int     `json:"sequence"`
	Size       int     `json:"size"`
	Latency    float64 `json:"latency"`
	AvgLatency float64 `json:"avg_latency"`
	Loss       float64 `json:"loss"`
}

// ICMP packet structure
type icmpPacket struct {
	Type        uint8
	Code        uint8
	Checksum    uint16
	Identifier  uint16
	SequenceNum uint16
	Data        []byte
}

// Calculate ICMP checksum according to RFC 792
func calculateChecksum(b []byte) uint16 {
	var sum uint32
	// Sum up all 16-bit words
	for i := 0; i < len(b); i += 2 {
		if i+1 < len(b) {
			sum += uint32(b[i])<<8 | uint32(b[i+1])
		} else {
			sum += uint32(b[i]) << 8 // Pad last byte with zero if needed
		}
	}
	// Add back any carries
	sum = (sum >> 16) + (sum & 0xffff)
	sum += sum >> 16

	// Return one's complement
	return uint16(^sum)
}

// Marshal ICMP packet to bytes according to RFC 792
func (p *icmpPacket) marshal() []byte {
	// ICMP header is 8 bytes: Type(1) + Code(1) + Checksum(2) + ID(2) + Seq(2)
	b := make([]byte, 8+len(p.Data))

	// Type 8 = Echo Request
	b[0] = 8

	// Code 0 = Echo Request
	b[1] = 0

	// Zero checksum field initially
	b[2] = 0
	b[3] = 0

	// Identifier (16 bits)
	binary.BigEndian.PutUint16(b[4:6], p.Identifier)

	// Sequence Number (16 bits)
	binary.BigEndian.PutUint16(b[6:8], p.SequenceNum)

	// Copy data after header
	if len(p.Data) > 0 {
		copy(b[8:], p.Data)
	}

	// Calculate checksum over entire message
	checksum := calculateChecksum(b)

	// Place checksum into bytes 2-3
	b[2] = byte(checksum >> 8)    // High byte
	b[3] = byte(checksum & 0xFF)  // Low byte

	return b
}

// IP_PMTUDISC_DO is used to always set the Don't Fragment bit
const IP_PMTUDISC_DO = 2

type PingConfig struct {
	count         int
	interval      int
	timeout       int
	size          int
	quiet         bool
	retries       int
	period        int
	source        string
	ipv6          bool
	parallel      bool
	maxBatch      int
	dontFragment  bool
}

func main() {
	var config PingConfig

	rootCmd := &cobra.Command{
		Use:   "jsonping [flags] target...",
		Short: "A ping utility that outputs results in JSON format",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// Handle parallel pinging
			if config.parallel {
				results := pingParallel(args, &config)
				if !config.quiet {
					fmt.Fprintln(os.Stderr, "All pings completed")
				}
				json.NewEncoder(os.Stdout).Encode(results)
			} else {
				// Single target mode
				target := args[0]
				result := pingHost(target, &config)
				json.NewEncoder(os.Stdout).Encode([]PingResult{result})
			}
		},
	}

	flags := rootCmd.Flags()
	flags.IntVarP(&config.count, "count", "c", 4, "Number of pings to send")
	flags.IntVarP(&config.interval, "interval", "i", 1000, "Interval between pings in milliseconds")
	flags.IntVarP(&config.timeout, "timeout", "W", 1000, "Time to wait for each response in milliseconds")
	flags.IntVarP(&config.size, "size", "s", 56, "Size of ping data in bytes")
	flags.BoolVarP(&config.quiet, "quiet", "q", false, "Quiet output")
	flags.IntVarP(&config.retries, "retry", "r", 0, "Number of retries for failed pings")
	flags.IntVarP(&config.period, "period", "p", 100, "Time between retries in milliseconds")
	flags.StringVar(&config.source, "source", "", "Source IP address")
	flags.BoolVarP(&config.ipv6, "ipv6", "6", false, "Use IPv6")
	flags.BoolVarP(&config.parallel, "parallel", "P", false, "Ping multiple targets in parallel")
	flags.IntVarP(&config.maxBatch, "max-batch", "B", 100, "Maximum number of parallel pings")
	flags.BoolVarP(&config.dontFragment, "dont-fragment", "M", false, "Set Don't Fragment bit")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// pingParallel pings multiple hosts in parallel batches
func pingParallel(targets []string, config *PingConfig) []PingResult {
	results := make([]PingResult, len(targets))
	batchSize := config.maxBatch
	if batchSize > len(targets) {
		batchSize = len(targets)
	}

	// Process targets in batches
	for i := 0; i < len(targets); i += batchSize {
		end := i + batchSize
		if end > len(targets) {
			end = len(targets)
		}

		// Create channels for each target in this batch
		resultChan := make(chan PingResult, end-i)

		// Start a goroutine for each target
		for j := i; j < end; j++ {
			go func(target string) {
				resultChan <- pingHost(target, config)
			}(targets[j])
		}

		// Collect results for this batch
		for j := i; j < end; j++ {
			results[j] = <-resultChan
		}
	}

	return results
}

func pingHost(target string, config *PingConfig) PingResult {
	result := PingResult{
		Target:    target,
		StartTime: time.Now(),
	}

	// Resolve target
	addr, err := net.ResolveIPAddr("ip4", target)
	if err != nil {
		result.Error = fmt.Sprintf("Could not resolve %v: %v", target, err)
		result.EndTime = time.Now()
		return result
	}

	// Create raw socket
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_ICMP)
	if err != nil {
		result.Error = fmt.Sprintf("Error creating raw socket: %v", err)
		result.EndTime = time.Now()
		return result
	}
	defer syscall.Close(fd)
	
	// Enable receiving ICMP error messages
	err = syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_RECVERR, 1)
	if err != nil {
		result.Error = fmt.Sprintf("Error setting IP_RECVERR: %v", err)
		result.EndTime = time.Now()
		return result
	}
	
	// Set Don't Fragment bit if requested
	if config.dontFragment {
		err = syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_MTU_DISCOVER, IP_PMTUDISC_DO)
		if err != nil {
			result.Error = fmt.Sprintf("Error setting DF bit: %v", err)
			result.EndTime = time.Now()
			return result
		}
	}

	// Set up signal handling
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	// Prepare statistics
	var latencies []float64
	result.Transmitted = 0

	// Ensure minimum packet size for our data
	minSize := 16 // 8 bytes for timestamp + 8 bytes for magic string
	if config.size < minSize {
		if !config.quiet {
			fmt.Fprintf(os.Stderr, "Warning: Increasing packet size from %d to %d bytes to accommodate timestamp and magic string\n", config.size, minSize)
		}
		config.size = minSize
	}

	// Create packet data
	data := make([]byte, config.size)
	
	// Next bytes: magic string to identify our ping
	copy(data[8:], []byte("JSONPING"))
	
	// Fill remaining with incrementing pattern
	for i := 16; i < config.size; i++ {
		data[i] = byte(i)
	}

	// Create ICMP Echo Request packet
	pid := os.Getpid() & 0xffff
	packet := &icmpPacket{
		Type:       8,  // Type 8 = Echo Request
		Code:       0,  // Code 0 = Echo Request
		Identifier: uint16(pid), // Use PID as identifier
		Data:       data,
	}

	for i := 0; i < config.count; i++ {
		select {
		case <-sig:
			result.EndTime = time.Now()
			return result
		default:
			// Wait for interval between pings
			if i > 0 {
				time.Sleep(time.Duration(config.interval) * time.Millisecond)
			}

			// Update sequence number
			packet.SequenceNum = uint16(i)
			msgBytes := packet.marshal()

			// Create destination address
			destAddr := syscall.SockaddrInet4{Port: 0}
			copy(destAddr.Addr[:], addr.IP.To4())

			// Store timestamp right before sending
			timestamp := time.Now().UnixNano()
			binary.BigEndian.PutUint64(data[0:8], uint64(timestamp))
			msgBytes = packet.marshal()

			// Send using raw socket
			err = syscall.Sendto(fd, msgBytes, 0, &destAddr)
			if err != nil {
				// Check if it's a fragmentation needed error
				if err == syscall.EMSGSIZE {
					// Try to find the interface MTU by looking up the route
					conn, err := net.Dial("udp", target+":0")
					if err == nil {
						localAddr := conn.LocalAddr().(*net.UDPAddr)
						conn.Close()

						ifaces, err := net.Interfaces()
						if err == nil {
							for _, iface := range ifaces {
								addrs, err := iface.Addrs()
								if err != nil {
									continue
								}
								for _, addr := range addrs {
									ipnet, ok := addr.(*net.IPNet)
									if !ok {
										continue
									}
									if ipnet.Contains(localAddr.IP) {
										// Found the interface for this connection
										result.Error = fmt.Sprintf("Fragmentation needed, interface MTU: %d bytes", iface.MTU)
										result.MTU = iface.MTU
										result.LossPercentage = 100.0
										result.EndTime = time.Now()
										return result
									}
								}
							}
						}
					}
					// If we couldn't find the interface, report the error without a specific MTU
					result.Error = "Fragmentation needed, packet too large"
					result.EndTime = time.Now()
					return result
				}
				fmt.Fprintf(os.Stderr, "Error sending ICMP packet: %v\n", err)
				continue
			}
			result.Transmitted++

			// Wait for reply with configured timeout
			reply := make([]byte, 1500)
			
			// Set read timeout
			err := setSocketTimeout(fd, config.timeout)
			if err != nil {
				result.Error = fmt.Sprintf("Error setting read timeout: %v", err)
				continue
			}

			// Read reply
			n, fromAddr, err := syscall.Recvfrom(fd, reply, 0)
			if err != nil {
				// Implement retry logic
				if config.retries > 0 {
					var retrySuccess bool
					for retry := 0; retry < config.retries; retry++ {
						time.Sleep(time.Duration(config.period) * time.Millisecond)
						
						// Retry send
						err = syscall.Sendto(fd, msgBytes, 0, &destAddr)
						if err != nil {
							continue
						}
						result.Transmitted++

						// Set socket timeout for retry
						err = setSocketTimeout(fd, config.timeout)
						if err != nil {
							continue
						}

						// Wait for reply
						n, fromAddr, err = syscall.Recvfrom(fd, reply, 0)
						if err == nil {
							retrySuccess = true
							break
						}
					}
					if !retrySuccess {
						continue
					}
				} else {
					continue
				}
			}

			// Convert from syscall.Sockaddr to syscall.SockaddrInet4
			fromAddr4, ok := fromAddr.(*syscall.SockaddrInet4)
			if !ok {
				continue
			}

			// Verify source address matches
			srcIP := net.IPv4(fromAddr4.Addr[0], fromAddr4.Addr[1], fromAddr4.Addr[2], fromAddr4.Addr[3])
			if !srcIP.Equal(addr.IP) {
				continue
			}

			// Check ICMP reply
			if n < 28 { // IP header (20) + ICMP header (8)
				continue
			}

			// Extract ICMP header fields
			icmpData := reply[20:] // Skip IP header
			icmpType := icmpData[0]     // Type (1 byte)
			icmpCode := icmpData[1]     // Code (1 byte)
			icmpId := binary.BigEndian.Uint16(icmpData[4:6])   // Identifier (2 bytes)
			icmpSeq := binary.BigEndian.Uint16(icmpData[6:8])  // Sequence number (2 bytes)

			// Verify Echo Reply
			if icmpType != 0 || icmpCode != 0 { // Type 0 = Echo Reply
				continue
			}

			// Verify identifier matches
			if icmpId != uint16(pid) {
				continue
			}

			// Verify sequence number matches
			if icmpSeq != uint16(i) {
				continue
			}

			// Extract timestamp from reply data
			if n < 44 { // IP(20) + ICMP(8) + Data(16 = 8 for timestamp + 8 for magic)
				if !config.quiet {
					fmt.Fprintf(os.Stderr, "Packet too small: %d bytes\n", n)
				}
				continue
			}

			// Verify magic string
			magic := string(icmpData[16:24])
			if magic != "JSONPING" {
				if !config.quiet {
					fmt.Fprintf(os.Stderr, "Invalid magic string: %q\n", magic)
				}
				continue
			}

			// Extract timestamp from reply data (which is our original timestamp)
			sentTime := int64(binary.BigEndian.Uint64(icmpData[8:16]))
			// Convert to time.Time for better precision
			sent := time.Unix(0, sentTime)
			latency := float64(time.Since(sent).Nanoseconds()) / float64(time.Millisecond)
			latencies = append(latencies, latency)

			// Update progress if not in quiet mode
			if !config.quiet {
				progress := Progress{
					Target:     target,
					Sequence:   i + 1,
					Size:       len(msgBytes),
					Latency:    latency,
					Loss:       float64(result.Transmitted-len(latencies)) / float64(result.Transmitted) * 100.0,
				}
				if len(latencies) > 0 {
					var sum float64
					for _, l := range latencies {
						sum += l
					}
					progress.AvgLatency = sum / float64(len(latencies))
				}
				json.NewEncoder(os.Stdout).Encode(progress)
			}
		}
	}

	// Calculate statistics
	result.Received = len(latencies)
	if result.Transmitted > 0 {
		result.LossPercentage = float64(result.Transmitted-result.Received) / float64(result.Transmitted) * 100.0
	} else {
		result.LossPercentage = 100.0
	}

	if result.Received > 0 {
		// Calculate min/avg/max latency
		result.MinLatency = latencies[0]
		result.MaxLatency = latencies[0]
		var sum float64
		for _, latency := range latencies {
			if latency < result.MinLatency {
				result.MinLatency = latency
			}
			if latency > result.MaxLatency {
				result.MaxLatency = latency
			}
			sum += latency
		}
		result.AvgLatency = sum / float64(result.Received)
	}

	result.EndTime = time.Now()
	return result
}


