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
	logFile := flag.String("log-file", "", "agent diagnostics file; default data-dir/agent.log")
	raidTool := flag.String("install-raid-tool", "", "stage a verified portable RAID CLI (storcli, storcli2, perccli, perccli2, ssacli)")
	raidSource := flag.String("raid-tool-source", "", "absolute path or HTTPS URL of the RAID executable or Dell PERCcli ZPE package")
	raidChecksum := flag.String("raid-tool-sha256", "", "SHA-256 of the RAID executable or package")
	raidIfDetected := flag.Bool("raid-tool-if-detected", false, "stage only if compatible RAID hardware is detected")
	flag.Parse()
	if *raidTool != "" || *raidSource != "" || *raidChecksum != "" || *raidIfDetected {
		path, err := collector.InstallRAIDTool(*raidTool, *raidSource, *raidChecksum, *raidIfDetected)
		if err != nil {
			log.Fatal(err)
		}
		if path == "" {
			log.Print("RAID CLI skipped: no matching RAID controller")
		} else {
			log.Printf("RAID CLI available at %s", path)
		}
		return
	}
	if runHardwareInventory() {
		return
	}
	// Write diagnostics before validating URL/token/agent ID, otherwise Task
	// Scheduler reports only LastTaskResult=1 without a useful error.
	log.SetFlags(log.LstdFlags | log.LUTC)
	logPath := *logFile
	if logPath == "" {
		logPath = filepath.Join(*data, "agent.log")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		log.Printf("cannot prepare log directory %q: %v", filepath.Dir(logPath), err)
	} else if file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); err != nil {
		log.Printf("cannot open diagnostic log %q: %v", logPath, err)
	} else {
		defer file.Close()
		log.SetOutput(io.MultiWriter(os.Stderr, file))
	}
	log.Printf("Storavix Agent starting version=%s os=%s data_dir=%q server=%q", telemetry.AgentVersion, runtime.GOOS, *data, *serverURL)
	if *interval < 15*time.Second {
		log.Fatal("interval must be at least 15s")
	}
	normalizedURL, e := normalizeServerURL(*serverURL, *allowHTTP)
	if e != nil {
		log.Fatal(e)
	}
	log.Printf("using central server=%q", normalizedURL)
	if *tokenFile == "" {
		log.Fatal("--token-file required")
	}
	tokenBytes, e := os.ReadFile(*tokenFile)
	if e != nil {
		log.Fatalf("unable to read token file %q: %v", *tokenFile, e)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if len(token) != 64 {
		log.Fatal("invalid registration token length (expected 64 hexadecimal characters)")
	}
	id, e := ensureID(*data)
	if e != nil {
		log.Fatalf("agent identity error in %q: %v", *data, e)
	}
	log.Printf("agent identity loaded")
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	hostname = strings.TrimSpace(hostname)
	if len(hostname) > 128 {
		hostname = hostname[:128]
	}
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}}
	if !*once && runtime.GOOS == "windows" {
		go runLocateWorker(client, normalizedURL, token, id)
	}
	osName := collector.OSName()
	agentStartedAt := time.Now()
	var sendFailures uint64
	var lastSendDurationMs *int64
	// The heartbeat must never be blocked by a slow CIM/PowerShell host-metrics
	// query. Host telemetry is collected independently and reused only while fresh.
	hostMetrics := newHostSampler(collector.Read)
	hostMetrics.start()
	type hardwareSample struct {
		inventory   collector.Inventory
		temperature *float64
	}
	var hardware *hardwareSample
	pollingStorage := true
	storage := make(chan hardwareSample, 1)
	scan := func() { storage <- hardwareSample{collector.ReadStorage(), collector.Temperature()} }
	go scan()
	for {
		if *once {
			// For a manual one-shot probe, try to include host metrics but do
			// not wait forever for a stuck Windows CIM provider.
			hostMetrics.waitForFirst(20 * time.Second)
		}
		r := hostMetrics.current(time.Now())
		select {
		case result := <-storage:
			hardware = &result
			pollingStorage = false
		default:
			if *once {
				result := <-storage
				hardware = &result
			}
		}
		if hardware != nil {
			hardware.inventory.AddTo(&r)
			r.Temperature = hardware.temperature
			if !*once && time.Since(hardware.inventory.CollectedAt) >= time.Minute && !pollingStorage {
				pollingStorage = true
				go scan()
			}
		}
		r.ID = id
		r.Hostname = hostname
		r.OS = runtime.GOOS
		r.OSName = osName
		r.Version = telemetry.AgentVersion
		diag := hostMetrics.diagnostics(time.Now())
		diag.AgentUptimeSeconds = int64(time.Since(agentStartedAt).Seconds())
		diag.ReportIntervalSeconds = int64(interval.Seconds())
		diag.SendFailures = sendFailures
		diag.LastSendDurationMs = lastSendDurationMs
		r.Diagnostics = diag
		sendStarted := time.Now()
		e = send(client, normalizedURL, token, r)
		elapsed := time.Since(sendStarted).Milliseconds()
		lastSendDurationMs = &elapsed
		if e != nil {
			sendFailures++
			log.Printf("report failed: %v", e)
		} else {
			sendFailures = 0
			log.Printf("reported host=%s os=%s metrics=%s sample_age=%v collector_ms=%v send_ms=%d",
				hostname, r.OS, diag.MetricsStatus, diag.SampleAgeSeconds, diag.SampleDurationMs, elapsed)
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

// normalizeServerURL accepts either form of a bare origin (with or without
// its trailing slash). It rejects paths, queries and embedded credentials.
func normalizeServerURL(raw string, allowHTTP bool) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", errors.New("--server must be a base URL like https://storavix.example.com[:port] (optional trailing slash)")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && allowHTTP) {
		return "", errors.New("HTTPS required. Use --allow-http only for isolated test networks")
	}
	// u.String() normalizes URL casing and escaping; remove only one optional '/'.
	return strings.TrimSuffix(u.String(), "/"), nil
}

func send(client *http.Client, base, token string, r telemetry.Report) error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	for {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/api/agent/report", strings.NewReader(string(b)))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Storavix-Request", "1")
		req.Header.Set("Content-Type", "application/json")
		ans, err := client.Do(req)
		if err != nil {
			return err
		}
		if ans.StatusCode == http.StatusAccepted {
			ans.Body.Close()
			return nil
		}
		msg, _ := io.ReadAll(io.LimitReader(ans.Body, 240))
		ans.Body.Close()
		message := strings.TrimSpace(string(msg))
		// Older Storavix servers reject unknown JSON fields before ingesting.
		if ans.StatusCode == http.StatusBadRequest && message == `{"error":"invalid report"}` {
			// Compatibility with servers older than 0.2.23.
			// Do not discard storage metrics or identity when stripping new fields.
			if r.Diagnostics != nil || r.CPUModel != "" || r.CPUCores != 0 || r.CPUThreads != 0 {
				r.Diagnostics = nil
				r.CPUModel = ""
				r.CPUCores = 0
				r.CPUThreads = 0
				continue
			}
			if r.Temperature != nil {
				r.Temperature = nil
				continue
			}
			if r.OSName != "" {
				r.OSName = ""
				continue
			}
		}
		return fmt.Errorf("server returned %d %s", ans.StatusCode, message)
	}
}
