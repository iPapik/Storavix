package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/iPapik/Storavix/internal/collector"
	"github.com/iPapik/Storavix/internal/telemetry"
)

func ensureID(dir string) (string, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	p := filepath.Join(dir, "agent.id")
	if b, e := os.ReadFile(p); e == nil {
		v := strings.TrimSpace(string(b))
		if len(v) != 32 {
			return "", errors.New("invalid agent.id")
		}
		return v, nil
	} else if !os.IsNotExist(e) {
		return "", e
	}
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	id := hex.EncodeToString(b)
	f, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return "", e
	}
	defer f.Close()
	if _, e = f.WriteString(id + "\n"); e != nil {
		return "", e
	}
	return id, nil
}
func main() {
	serverURL := flag.String("server", "", "Storavix URL (https://host:8080)")
	tokenFile := flag.String("token-file", "", "file with registration token")
	data := flag.String("data-dir", "./agent-data", "agent identity directory")
	interval := flag.Duration("interval", 30*time.Second, "polling interval, minimum 15s")
	once := flag.Bool("once", false, "send a single report")
	allowHTTP := flag.Bool("allow-http", false, "INSECURE: allow unencrypted transport on a trusted network only")
	flag.Parse()
	if *interval < 15*time.Second {
		log.Fatal("interval must be at least 15s")
	}
	u, e := url.Parse(*serverURL)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		log.Fatal("--server must be a base URL, for example https://storavix.example.com")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && *allowHTTP) {
		log.Fatal("HTTPS required. Use --allow-http only for isolated test networks")
	}
	if *tokenFile == "" {
		log.Fatal("--token-file required")
	}
	tokenBytes, e := os.ReadFile(*tokenFile)
	if e != nil {
		log.Fatal(e)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if len(token) < 32 {
		log.Fatal("invalid token")
	}
	id, e := ensureID(*data)
	if e != nil {
		log.Fatal(e)
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	hostname = strings.TrimSpace(hostname)
	if len(hostname) > 128 {
		hostname = hostname[:128]
	}
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}}
	var prev collector.Snapshot
	var last time.Time
	for {
		r, current := collector.Read()
		r.ID = id
		r.Hostname = hostname
		r.OS = runtime.GOOS
		r.Version = telemetry.AgentVersion
		now := time.Now()
		// Network and CPU counters need consecutive samples; the first sample is unknown, never fake 0.
		if !last.IsZero() {
			elapsed := now.Sub(last).Seconds()
			if elapsed > 0 && current.NetValid && prev.NetValid && current.NetBytes >= prev.NetBytes {
				x := float64(current.NetBytes-prev.NetBytes) / elapsed
				r.NetworkBytesPerSec = &x
			}
			if runtime.GOOS == "linux" && current.CPUValid && prev.CPUValid && current.CPUTotal > prev.CPUTotal && current.CPUIdle >= prev.CPUIdle {
				x := 100 * (1 - float64(current.CPUIdle-prev.CPUIdle)/float64(current.CPUTotal-prev.CPUTotal))
				if x >= 0 && x <= 100 {
					r.CPUPercent = &x
				}
			}
		}
		prev = current
		last = now
		if e := send(client, *serverURL, token, r); e != nil {
			log.Printf("report failed: %v", e)
		} else {
			log.Printf("reported host=%s os=%s", hostname, r.OS)
		}
		if *once {
			if e != nil {
				os.Exit(1)
			}
			return
		}
		time.Sleep(*interval)
	}
}
func send(client *http.Client, base, token string, r telemetry.Report) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/api/agent/report", strings.NewReader(string(b)))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Storavix-Request", "1")
	req.Header.Set("Content-Type", "application/json")
	ans, e := client.Do(req)
	if e != nil {
		return e
	}
	defer ans.Body.Close()
	if ans.StatusCode != http.StatusAccepted {
		msg, _ := io.ReadAll(io.LimitReader(ans.Body, 240))
		return fmt.Errorf("server returned %d %s", ans.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}
