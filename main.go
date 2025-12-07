package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

// Config holds the application configuration, loaded from environment variables.
type Config struct {
	ProxyHost          string
	ProxyPort          int
	TargetHost         string
	TargetPort         int
	TargetMAC          string
	TargetBroadcastIP  string
	ConnectionRetries  int
	RetryDelaySeconds  time.Duration
}

// loadConfig loads configuration from environment variables with defaults.
func loadConfig() (*Config, error) {
	// Helper to get an environment variable or return a default value.
	getEnv := func(key, fallback string) string {
		if value, exists := os.LookupEnv(key); exists {
			return value
		}
		return fallback
	}

	// Helper to get an integer environment variable.
	getEnvAsInt := func(key string, fallback int) (int, error) {
		strValue := getEnv(key, "")
		if strValue == "" {
			return fallback, nil
		}
		val, err := strconv.Atoi(strValue)
		if err != nil {
			return 0, fmt.Errorf("invalid value for %s: %v", key, err)
		}
		return val, nil
	}
	
	// Required fields
	targetHost := os.Getenv("TARGET_HOST")
	targetMAC := os.Getenv("TARGET_MAC")
	if targetHost == "" || targetMAC == "" {
		return nil, fmt.Errorf("TARGET_HOST and TARGET_MAC environment variables are required")
	}

	proxyPort, err := getEnvAsInt("PROXY_PORT", 2222)
	if err != nil { return nil, err }
	
	targetPort, err := getEnvAsInt("TARGET_PORT", 22)
	if err != nil { return nil, err }

	connectionRetries, err := getEnvAsInt("CONNECTION_RETRIES", 15)
	if err != nil { return nil, err }

	retryDelay, err := getEnvAsInt("RETRY_DELAY_SECONDS", 5)
	if err != nil { return nil, err }


	return &Config{
		ProxyHost:          getEnv("PROXY_HOST", "0.0.0.0"),
		ProxyPort:          proxyPort,
		TargetHost:         targetHost,
		TargetPort:         targetPort,
		TargetMAC:          targetMAC,
		TargetBroadcastIP:  getEnv("TARGET_BROADCAST_IP", "255.255.255.255"),
		ConnectionRetries:  connectionRetries,
		RetryDelaySeconds:  time.Duration(retryDelay) * time.Second,
	}, nil
}


// createMagicPacket creates a Wake-on-LAN magic packet from a MAC address string.
func createMagicPacket(macAddr string) ([]byte, error) {
	hwAddr, err := net.ParseMAC(macAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid MAC address format: %w", err)
	}

	// Magic packet is 6 bytes of 0xFF followed by 16 repetitions of the MAC address
	packet := make([]byte, 6, 102)
	for i := 0; i < 6; i++ {
		packet[i] = 0xFF
	}

	for i := 0; i < 16; i++ {
		packet = append(packet, hwAddr...)
	}

	return packet, nil
}


// sendWOLPacket constructs and sends the Wake-on-LAN packet.
func sendWOLPacket(macAddr, broadcastIP string) error {
	magicPacket, err := createMagicPacket(macAddr)
	if err != nil {
		return err
	}

	// The destination for a WOL packet is typically port 9
	addr := fmt.Sprintf("%s:9", broadcastIP)
	
	// We don't need a specific local port, so we can use ":0"
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to dial UDP for WOL: %w", err)
	}
	defer conn.Close()

	bytesWritten, err := conn.Write(magicPacket)
	if err != nil {
		return fmt.Errorf("failed to write magic packet: %w", err)
	}

	log.Printf("Sent %d byte Wake-on-LAN packet to %s for MAC %s", bytesWritten, broadcastIP, macAddr)
	return nil
}

// proxyTraffic bi-directionally copies data between two connections.
func proxyTraffic(client, target net.Conn) {
	log.Printf("Starting traffic proxy between %s and %s", client.RemoteAddr(), target.RemoteAddr())
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer target.Close() // Ensure the other connection is closed on exit
		io.Copy(target, client)
	}()

	go func() {
		defer wg.Done()
		defer client.Close() // Ensure the other connection is closed on exit
		io.Copy(client, target)
	}()
	
	wg.Wait()
	log.Printf("Proxy connection between %s and %s closed.", client.RemoteAddr(), target.RemoteAddr())
}

// handleClient manages an incoming client connection.
func handleClient(clientConn net.Conn, cfg *Config) {
	defer clientConn.Close()
	log.Printf("Accepted connection from %s", clientConn.RemoteAddr())

	// 1. Send Wake-on-LAN packet
	if err := sendWOLPacket(cfg.TargetMAC, cfg.TargetBroadcastIP); err != nil {
		log.Printf("Error sending WOL packet: %v", err)
		return
	}

	// 2. Wait and attempt to connect to the target SSH server
	var targetConn net.Conn
	var err error
	targetAddr := fmt.Sprintf("%s:%d", cfg.TargetHost, cfg.TargetPort)
	
	log.Printf("Attempting to connect to target %s...", targetAddr)
	for i := 0; i < cfg.ConnectionRetries; i++ {
		targetConn, err = net.DialTimeout("tcp", targetAddr, cfg.RetryDelaySeconds)
		if err == nil {
			log.Printf("Successfully connected to target %s on attempt %d.", targetAddr, i+1)
			break
		}
		log.Printf("Attempt %d/%d failed to connect to target: %v. Retrying in %v...", i+1, cfg.ConnectionRetries, err, cfg.RetryDelaySeconds)
		time.Sleep(cfg.RetryDelaySeconds)
	}

	if targetConn == nil {
		log.Printf("Could not connect to target server after %d attempts. Closing client connection.", cfg.ConnectionRetries)
		return
	}
	defer targetConn.Close()

	// 3. Start proxying traffic
	proxyTraffic(clientConn, targetConn)
}


func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	listenAddr := fmt.Sprintf("%s:%d", cfg.ProxyHost, cfg.ProxyPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to start listener on %s: %v", listenAddr, err)
	}
	defer listener.Close()
	log.Printf("SSH WOL Proxy server listening on %s", listenAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		// Handle each client connection in a new goroutine
		go handleClient(conn, cfg)
	}
}
