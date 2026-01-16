package metrics

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestPrometheus_ScanningMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	p := NewPrometheus(reg)

	// Test IncFilesScanned
	p.IncFilesScanned("/tmp")
	p.IncFilesScanned("/tmp")
	p.IncFilesScanned("/var")

	assertCounterValue(t, p.filesScanned, []string{"/tmp"}, 2)
	assertCounterValue(t, p.filesScanned, []string{"/var"}, 1)

	// Test IncDirsScanned
	p.IncDirsScanned("/tmp")
	assertCounterValue(t, p.dirsScanned, []string{"/tmp"}, 1)

	// Test ObserveScanDuration
	p.ObserveScanDuration("/tmp", 5*time.Second)
	p.ObserveScanDuration("/tmp", 10*time.Second)

	// Verify histogram has observations by gathering metrics
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	var found bool
	for _, mf := range mfs {
		if mf.GetName() == "storagesage_scanner_scan_duration_seconds" {
			for _, m := range mf.GetMetric() {
				for _, label := range m.GetLabel() {
					if label.GetName() == "root" && label.GetValue() == "/tmp" {
						found = true
						if m.Histogram.GetSampleCount() != 2 {
							t.Errorf("expected 2 histogram samples, got %d", m.Histogram.GetSampleCount())
						}
						// Sum should be 15 seconds
						if m.Histogram.GetSampleSum() != 15.0 {
							t.Errorf("expected sum of 15.0, got %f", m.Histogram.GetSampleSum())
						}
					}
				}
			}
		}
	}
	if !found {
		t.Error("scan duration histogram metric not found")
	}
}

func TestPrometheus_PlanningMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	p := NewPrometheus(reg)

	// Test IncPolicyDecision
	p.IncPolicyDecision("age_ok", true)
	p.IncPolicyDecision("age_ok", true)
	p.IncPolicyDecision("too_new", false)

	assertCounterValue(t, p.policyDecisions, []string{"age_ok", "true"}, 2)
	assertCounterValue(t, p.policyDecisions, []string{"too_new", "false"}, 1)

	// Test IncSafetyVerdict
	p.IncSafetyVerdict("allowed", true)
	p.IncSafetyVerdict("protected", false)
	assertCounterValue(t, p.safetyVerdicts, []string{"allowed", "true"}, 1)
	assertCounterValue(t, p.safetyVerdicts, []string{"protected", "false"}, 1)

	// Test SetBytesEligible
	p.SetBytesEligible(1024 * 1024)
	assertGaugeValue(t, p.bytesEligible, 1024*1024)

	// Test SetFilesEligible
	p.SetFilesEligible(42)
	assertGaugeValue(t, p.filesEligible, 42)
}

func TestPrometheus_ExecutionMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	p := NewPrometheus(reg)

	// Test IncFilesDeleted
	p.IncFilesDeleted("/tmp")
	p.IncFilesDeleted("/tmp")
	assertCounterValue(t, p.filesDeleted, []string{"/tmp"}, 2)

	// Test IncDirsDeleted
	p.IncDirsDeleted("/var")
	assertCounterValue(t, p.dirsDeleted, []string{"/var"}, 1)

	// Test AddBytesFreed
	p.AddBytesFreed(1000)
	p.AddBytesFreed(2000)
	metric := &dto.Metric{}
	if err := p.bytesFreed.Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if metric.Counter.GetValue() != 3000 {
		t.Errorf("expected 3000 bytes freed, got %f", metric.Counter.GetValue())
	}

	// Test IncDeleteErrors
	p.IncDeleteErrors("permission_denied")
	p.IncDeleteErrors("permission_denied")
	p.IncDeleteErrors("not_found")
	assertCounterValue(t, p.deleteErrors, []string{"permission_denied"}, 2)
	assertCounterValue(t, p.deleteErrors, []string{"not_found"}, 1)
}

func TestPrometheus_SystemMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	p := NewPrometheus(reg)

	// Test SetDiskUsage
	p.SetDiskUsage(75.5)
	assertGaugeValue(t, p.diskUsage, 75.5)

	// Test SetCPUUsage
	p.SetCPUUsage(25.0)
	assertGaugeValue(t, p.cpuUsage, 25.0)

	// Test overwriting gauge values
	p.SetDiskUsage(80.0)
	assertGaugeValue(t, p.diskUsage, 80.0)
}

func TestPrometheus_ConcurrentUpdates(t *testing.T) {
	reg := prometheus.NewRegistry()
	p := NewPrometheus(reg)

	const goroutines = 10
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				p.IncFilesScanned("/concurrent")
				p.IncPolicyDecision("test", true)
				p.AddBytesFreed(1)
			}
		}()
	}

	wg.Wait()

	// Verify final counts
	assertCounterValue(t, p.filesScanned, []string{"/concurrent"}, float64(goroutines*iterations))
	assertCounterValue(t, p.policyDecisions, []string{"test", "true"}, float64(goroutines*iterations))

	metric := &dto.Metric{}
	if err := p.bytesFreed.Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	expected := float64(goroutines * iterations)
	if metric.Counter.GetValue() != expected {
		t.Errorf("expected %f bytes freed, got %f", expected, metric.Counter.GetValue())
	}
}

func TestPrometheus_DefaultRegistry(t *testing.T) {
	// Create with nil registry should use default
	p := NewPrometheus(nil)
	if p == nil {
		t.Fatal("expected non-nil Prometheus instance")
	}

	// Should be able to call methods without panic
	p.IncFilesScanned("/test")
	p.SetDiskUsage(50.0)
}

func TestBoolStr(t *testing.T) {
	if boolStr(true) != "true" {
		t.Errorf("expected 'true', got %q", boolStr(true))
	}
	if boolStr(false) != "false" {
		t.Errorf("expected 'false', got %q", boolStr(false))
	}
}

// assertCounterValue checks a counter vec has expected value for given labels
func assertCounterValue(t *testing.T, cv *prometheus.CounterVec, labels []string, expected float64) {
	t.Helper()
	metric := &dto.Metric{}
	if err := cv.WithLabelValues(labels...).Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if metric.Counter.GetValue() != expected {
		t.Errorf("expected counter value %f, got %f", expected, metric.Counter.GetValue())
	}
}

// assertGaugeValue checks a gauge has expected value
func assertGaugeValue(t *testing.T, g prometheus.Gauge, expected float64) {
	t.Helper()
	metric := &dto.Metric{}
	if err := g.Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}
	if metric.Gauge.GetValue() != expected {
		t.Errorf("expected gauge value %f, got %f", expected, metric.Gauge.GetValue())
	}
}
