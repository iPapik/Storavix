package main

import (
	"log"
	"runtime"
	"time"

	"github.com/iPapik/Storavix/internal/collector"
	"github.com/iPapik/Storavix/internal/telemetry"
)

// hostMetricsMaxAge bounds how long a value from CIM, WMI or other host
// collectors can be reused. An overdue poll must not block heartbeats, and
// missing metrics must not be represented as fresh values.
const hostMetricsMaxAge = 90 * time.Second

type hostSample struct {
	report telemetry.Report
	counters collector.Snapshot
	at time.Time
	took time.Duration
}

// hostSampler is owned by the sending goroutine. Exactly one collector call
// can be in flight; its result is delivered on a buffered channel, so even a
// permanently wedged CIM query cannot create an unbounded number of workers.
type hostSampler struct {
	read func() (telemetry.Report, collector.Snapshot)
	results chan hostSample
	running bool
	seen bool
	last hostSample
	stale bool
}

func newHostSampler(read func() (telemetry.Report, collector.Snapshot)) *hostSampler {
	return &hostSampler{read:read, results:make(chan hostSample,1)}
}

func (s *hostSampler) start() {
	if s.running { return }
	s.running = true
	go func() {
		started := time.Now()
		r, counters := s.read()
		s.results <- hostSample{report:r, counters:counters, at:time.Now(), took:time.Since(started)}
	}()
}

func (s *hostSampler) accept(next hostSample) {
	s.running = false
	if next.took >= 12*time.Second {
		log.Printf("host metrics collection slow: %s", next.took.Round(time.Second))
	}
	// Rate calculations are done only when a NEW host sample is available,
	// never on a repeated cached sample.
	if s.seen {
		elapsed := next.at.Sub(s.last.at).Seconds()
		if elapsed > 0 && next.counters.NetValid && s.last.counters.NetValid &&
			next.counters.NetBytes >= s.last.counters.NetBytes {
			rate := float64(next.counters.NetBytes-s.last.counters.NetBytes) / elapsed
			next.report.NetworkBytesPerSec = &rate
		}
		if runtime.GOOS == "linux" && next.counters.CPUValid && s.last.counters.CPUValid &&
			next.counters.CPUTotal > s.last.counters.CPUTotal &&
			next.counters.CPUIdle >= s.last.counters.CPUIdle {
			load := 100 * (1 - float64(next.counters.CPUIdle-s.last.counters.CPUIdle)/
				float64(next.counters.CPUTotal-s.last.counters.CPUTotal))
			if load >= 0 && load <= 100 {
				next.report.CPUPercent = &load
			}
		}
	}
	s.last = next
	s.seen = true
}

func (s *hostSampler) consumeReady() {
	select {
	case next := <-s.results:
		s.accept(next)
	default:
	}
}

func (s *hostSampler) waitForFirst(timeout time.Duration) {
	if s.seen { return }
	select {
	case next := <-s.results:
		s.accept(next)
	case <-time.After(timeout):
		log.Printf("host metrics did not arrive within %s; sending heartbeat without host metrics", timeout)
	}
}

func (s *hostSampler) current(now time.Time) telemetry.Report {
	s.consumeReady()
	if !s.running {
		s.start()
	}
	fresh := s.seen && !now.Before(s.last.at) && now.Sub(s.last.at) <= hostMetricsMaxAge
	if !fresh {
		if !s.stale {
			if s.seen {
				log.Printf("host metrics stale for %s; sending heartbeat without stale measurements", now.Sub(s.last.at).Round(time.Second))
			} else {
				log.Printf("host metrics pending; sending heartbeat without host measurements")
			}
			s.stale = true
		}
		return telemetry.Report{OS:runtime.GOOS}
	}
	if s.stale {
		log.Printf("host metrics resumed (latest sample age %s)", now.Sub(s.last.at).Round(time.Second))
		s.stale = false
	}
	// The returned Report contains scalar fields and slices. The sender
	// extends the copy, never mutating the cached snapshot.
	return s.last.report
}

// diagnostics explains whether OS statistics are current. This is kept apart
// from the presence report so a stalled CIM/WMI call cannot stall the heartbeat.
func (s *hostSampler) diagnostics(now time.Time) *telemetry.AgentDiagnostics {
	d := &telemetry.AgentDiagnostics{MetricsStatus:"pending"}
	if !s.seen {return d}
	if now.Before(s.last.at) {return d}
	d.SampleCollectedAt=s.last.at.UTC()
	age:=int64(now.Sub(s.last.at).Seconds())
	d.SampleAgeSeconds=&age
	duration:=s.last.took.Milliseconds()
	d.SampleDurationMs=&duration
	if now.Sub(s.last.at)>hostMetricsMaxAge {
		d.MetricsStatus="stale"
	} else {
		d.MetricsStatus="ok"
	}
	return d
}
