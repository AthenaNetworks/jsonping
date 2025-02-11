package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
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
	config := PingConfig{}

	cmd := &cobra.Command{
		Use:   "jsonping [targets...]",
		Short: "A simple ping utility with JSON output",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var results []PingResult

			if config.parallel {
				// Process targets in parallel batches
				results = pingParallel(args, &config)
			} else {
				// Process targets sequentially
				for _, target := range args {
					result := pingHost(target, &config)
					results = append(results, result)
				}
			}

			// Output JSON
			json, err := json.Marshal(results)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
				os.Exit(1)
			}
			fmt.Println(string(json))
		},
	}

	cmd.Flags().IntVarP(&config.count, "count", "c", 10, "number of pings to send")
	cmd.Flags().IntVarP(&config.interval, "interval", "i", 1000, "interval between pings in milliseconds")
	cmd.Flags().IntVarP(&config.timeout, "timeout", "t", 5000, "timeout in milliseconds for each ping")
	cmd.Flags().IntVarP(&config.size, "size", "s", 56, "size of ping packet payload in bytes")
	cmd.Flags().BoolVarP(&config.quiet, "quiet", "q", false, "only show summary statistics")
	cmd.Flags().IntVarP(&config.retries, "retry", "r", 0, "number of retries for failed pings")
	cmd.Flags().IntVarP(&config.period, "period", "p", 1000, "time in milliseconds between ping retries")
	cmd.Flags().StringVarP(&config.source, "source", "S", "", "source IP address")
	cmd.Flags().BoolVarP(&config.ipv6, "ipv6", "6", false, "use IPv6 instead of IPv4")
	cmd.Flags().BoolVarP(&config.parallel, "parallel", "a", false, "ping hosts in parallel")
	cmd.Flags().IntVarP(&config.maxBatch, "batch", "B", 100, "max hosts to ping in parallel (with -a)")
	cmd.Flags().BoolVarP(&config.dontFragment, "dont-fragment", "M", false, "set don't fragment bit")

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// pingParallel pings multiple hosts in parallel batches
func pingParallel(targets []string, config *PingConfig) []PingResult {
	var results []PingResult
	resultChan := make(chan PingResult)
	semaphore := make(chan struct{}, config.maxBatch)

	// Start a goroutine for each target
	for _, target := range targets {
		go func(t string) {
			semaphore <- struct{}{} // Acquire semaphore
			result := pingHost(t, config)
			resultChan <- result
			<-semaphore // Release semaphore
		}(target)
	}

	// Collect results
	for i := 0; i < len(targets); i++ {
		result := <-resultChan
		results = append(results, result)
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

	// Create packet data
	data := make([]byte, config.size)
	
	// First 8 bytes: timestamp for RTT calculation
	timestamp := time.Now().UnixNano()
	binary.BigEndian.PutUint64(data[0:8], uint64(timestamp))
	
	// Next bytes: magic string to identify our ping
	copy(data[8:], []byte("JSONPING"))
	
	// Fill remaining with incrementing pattern
	for i := 8 + len("JSONPING"); i < config.size; i++ {
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

			// Send ping
			start := time.Now()
			
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
			tv := syscall.Timeval{
				Sec:  int64(config.timeout / 1000),
				Usec: int64((config.timeout % 1000) * 1000),
			}
			err = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
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

			// For Echo Reply, verify ID and sequence
			icmpId := binary.BigEndian.Uint16(icmpData[4:6])       // ID (2 bytes)
			icmpSeq := binary.BigEndian.Uint16(icmpData[6:8])      // Sequence (2 bytes)

			// Verify this is an Echo Reply with correct code and matching ID/SEQ
			if icmpType != 0 || icmpCode != 0 || icmpId != packet.Identifier || icmpSeq != packet.SequenceNum {
				continue
			}

			// Calculate latency
			duration := float64(time.Since(start).Nanoseconds()) / 1_000_000.0 // Convert nanoseconds to milliseconds
			latencies = append(latencies, duration)
			result.Received++

			// Print progress if not quiet
			if !config.quiet {
				// Calculate average latency
				avg := duration
				if len(latencies) > 1 {
					sum := 0.0
					for _, lat := range latencies {
						sum += lat
					}
					avg = sum / float64(len(latencies))
				}

				// Calculate loss percentage
				loss := 0.0
				if result.Transmitted > 0 {
					loss = 100.0 - (float64(result.Received)/float64(result.Transmitted))*100.0
				}

				// Create progress struct
				progress := Progress{
					Target:     target,
					Sequence:   i + 1,
					Size:       len(msgBytes),
					Latency:    duration,
					AvgLatency: avg,
					Loss:       loss,
				}

				// Marshal to JSON and print
				progressJSON, _ := json.Marshal(progress)
				fmt.Println(string(progressJSON))
			}

			// Wait for interval
			if i < config.count-1 { // Don't wait after last ping
				time.Sleep(time.Duration(config.interval) * time.Millisecond)
			}
		}
	}

	// Calculate statistics
	result.EndTime = time.Now()
	if len(latencies) > 0 {
		result.MinLatency = latencies[0]
		result.MaxLatency = latencies[0]
		sum := latencies[0]

		for _, lat := range latencies[1:] {
			sum += lat
			result.MinLatency = math.Min(result.MinLatency, lat)
			result.MaxLatency = math.Max(result.MaxLatency, lat)
		}

		result.AvgLatency = sum / float64(len(latencies))
	} else {
		// Set to 0 if no successful pings
		result.MinLatency = 0
		result.MaxLatency = 0
		result.AvgLatency = 0
	}

	// Calculate loss percentage
	if result.Transmitted > 0 {
		result.LossPercentage = 100 - (float64(result.Received)/float64(result.Transmitted))*100
	} else {
		result.LossPercentage = 100
	}

	return result
}
