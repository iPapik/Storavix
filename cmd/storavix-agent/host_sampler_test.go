package main

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iPapik/Storavix/internal/collector"
	"github.com/iPapik/Storavix/internal/telemetry"
)

func TestHostSamplerDoesNotBlockHeartbeatOnHungMetrics(t *testing.T) {
	blocked := make(chan struct{})
	var calls int32
	s := newHostSampler(func() (telemetry.Report,collector.Snapshot) {
		atomic.AddInt32(&calls,1)
		<-blocked
		return telemetry.Report{OS:"windows", MemoryBytes:1024}, collector.Snapshot{}
	})
	now:=time.Now()
	started:=time.Now()
	for i:=0;i<10;i++ {
		report:=s.current(now.Add(time.Duration(i)*30*time.Second))
		if report.OS!=runtime.GOOS || report.MemoryBytes!=0 {
			t.Fatalf("heartbeat %d carried stale metrics: %+v",i,report)
		}
	}
	if time.Since(started)>500*time.Millisecond {
		t.Fatal("heartbeat was blocked by host collector")
	}
	// Wait for the first goroutine to enter the collection, then verify
	// repeated heartbeats have not spawned additional collectors.
	deadline:=time.Now().Add(time.Second)
	for atomic.LoadInt32(&calls)==0 && time.Now().Before(deadline) {time.Sleep(time.Millisecond)}
	if calls!=1 {t.Fatalf("unbounded host collectors: %d",calls)}
	close(blocked)
	s.waitForFirst(time.Second)
	r:=s.current(time.Now())
	if r.MemoryBytes!=1024 {t.Fatalf("new metrics were lost: %+v",r)}
}

func TestHostSamplerExpiredValuesNotReused(t *testing.T) {
	s:=newHostSampler(func()(telemetry.Report,collector.Snapshot){select{} })
	s.running=true
	s.seen=true
	s.last=hostSample{report:telemetry.Report{MemoryBytes:8192},at:time.Now().Add(-2*time.Minute)}
	report:=s.current(time.Now())
	if report.MemoryBytes!=0 {t.Fatalf("expired metrics leaked into heartbeat: %+v",report)}
}

func TestHostSamplerComputesRateOncePerSample(t *testing.T) {
	s:=newHostSampler(func()(telemetry.Report,collector.Snapshot){return telemetry.Report{},collector.Snapshot{}})
	s.running=true
	start:=time.Now()
	s.results<-hostSample{report:telemetry.Report{OS:"linux"},counters:collector.Snapshot{NetBytes:100,NetValid:true},at:start}
	s.current(start.Add(time.Millisecond))
	s.running=true
	s.results<-hostSample{report:telemetry.Report{OS:"linux"},counters:collector.Snapshot{NetBytes:1100,NetValid:true},at:start.Add(10*time.Second)}
	r:=s.current(start.Add(10*time.Second))
	if r.NetworkBytesPerSec==nil || *r.NetworkBytesPerSec!=100 {
		t.Fatalf("bad network rate: %+v",r.NetworkBytesPerSec)
	}
	s.running=true
	r=s.current(start.Add(20*time.Second))
	if r.NetworkBytesPerSec==nil || *r.NetworkBytesPerSec!=100 {
		t.Fatal("cached counters recalculated without new sample")
	}
}
