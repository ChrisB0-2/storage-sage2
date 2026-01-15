package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// Prometheus implements core.Metrics using Prometheus client.
type Prometheus struct {
	// Scanning metrics
	filesScanned *prometheus.CounterVec
	dirsScanned  *prometheus.CounterVec
	scanDuration *prometheus.HistogramVec

	// Planning metrics
	policyDecisions *prometheus.CounterVec
	safetyVerdicts  *prometheus.CounterVec
	bytesEligible   prometheus.Gauge
	filesEligible   prometheus.Gauge

	// Execution metrics
	filesDeleted *prometheus.CounterVec
	dirsDeleted  *prometheus.CounterVec
	bytesFreed   prometheus.Counter
	deleteErrors *prometheus.CounterVec

	// System metrics
	diskUsage prometheus.Gauge
	cpuUsage  prometheus.Gauge
}

// NewPrometheus creates a new Prometheus metrics collector.
// All metrics are registered with the provided registry.
// If reg is nil, prometheus.DefaultRegisterer is used.
func NewPrometheus(reg prometheus.Registerer) *Prometheus {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	factory := promauto.With(reg)

	return &Prometheus{
		// Scanning metrics
		filesScanned: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "storagesage",
			Subsystem: "scanner",
			Name:      "files_scanned_total",
			Help:      "Total number of files scanned",
		}, []string{"root"}),

		dirsScanned: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "storagesage",
			Subsystem: "scanner",
			Name:      "dirs_scanned_total",
			Help:      "Total number of directories scanned",
		}, []string{"root"}),

		scanDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "storagesage",
			Subsystem: "scanner",
			Name:      "scan_duration_seconds",
			Help:      "Time spent scanning roots",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 10), // 0.1s to ~100s
		}, []string{"root"}),

		// Planning metrics
		policyDecisions: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "storagesage",
			Subsystem: "planner",
			Name:      "policy_decisions_total",
			Help:      "Total policy decisions by reason and outcome",
		}, []string{"reason", "allowed"}),

		safetyVerdicts: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "storagesage",
			Subsystem: "planner",
			Name:      "safety_verdicts_total",
			Help:      "Total safety verdicts by reason and outcome",
		}, []string{"reason", "allowed"}),

		bytesEligible: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: "storagesage",
			Subsystem: "planner",
			Name:      "bytes_eligible",
			Help:      "Total bytes eligible for deletion in current plan",
		}),

		filesEligible: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: "storagesage",
			Subsystem: "planner",
			Name:      "files_eligible",
			Help:      "Total files eligible for deletion in current plan",
		}),

		// Execution metrics
		filesDeleted: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "storagesage",
			Subsystem: "executor",
			Name:      "files_deleted_total",
			Help:      "Total number of files deleted",
		}, []string{"root"}),

		dirsDeleted: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "storagesage",
			Subsystem: "executor",
			Name:      "dirs_deleted_total",
			Help:      "Total number of directories deleted",
		}, []string{"root"}),

		bytesFreed: factory.NewCounter(prometheus.CounterOpts{
			Namespace: "storagesage",
			Subsystem: "executor",
			Name:      "bytes_freed_total",
			Help:      "Total bytes freed by deletions",
		}),

		deleteErrors: factory.NewCounterVec(prometheus.CounterOpts{
			Namespace: "storagesage",
			Subsystem: "executor",
			Name:      "delete_errors_total",
			Help:      "Total delete errors by reason",
		}, []string{"reason"}),

		// System metrics
		diskUsage: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: "storagesage",
			Subsystem: "system",
			Name:      "disk_usage_percent",
			Help:      "Current disk usage percentage",
		}),

		cpuUsage: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: "storagesage",
			Subsystem: "system",
			Name:      "cpu_usage_percent",
			Help:      "Current CPU usage percentage",
		}),
	}
}

// Scanning metrics

func (p *Prometheus) IncFilesScanned(root string) {
	p.filesScanned.WithLabelValues(root).Inc()
}

func (p *Prometheus) IncDirsScanned(root string) {
	p.dirsScanned.WithLabelValues(root).Inc()
}

func (p *Prometheus) ObserveScanDuration(root string, duration time.Duration) {
	p.scanDuration.WithLabelValues(root).Observe(duration.Seconds())
}

// Planning metrics

func (p *Prometheus) IncPolicyDecision(reason string, allowed bool) {
	p.policyDecisions.WithLabelValues(reason, boolStr(allowed)).Inc()
}

func (p *Prometheus) IncSafetyVerdict(reason string, allowed bool) {
	p.safetyVerdicts.WithLabelValues(reason, boolStr(allowed)).Inc()
}

func (p *Prometheus) SetBytesEligible(bytes int64) {
	p.bytesEligible.Set(float64(bytes))
}

func (p *Prometheus) SetFilesEligible(count int) {
	p.filesEligible.Set(float64(count))
}

// Execution metrics

func (p *Prometheus) IncFilesDeleted(root string) {
	p.filesDeleted.WithLabelValues(root).Inc()
}

func (p *Prometheus) IncDirsDeleted(root string) {
	p.dirsDeleted.WithLabelValues(root).Inc()
}

func (p *Prometheus) AddBytesFreed(bytes int64) {
	p.bytesFreed.Add(float64(bytes))
}

func (p *Prometheus) IncDeleteErrors(reason string) {
	p.deleteErrors.WithLabelValues(reason).Inc()
}

// System metrics

func (p *Prometheus) SetDiskUsage(percent float64) {
	p.diskUsage.Set(percent)
}

func (p *Prometheus) SetCPUUsage(percent float64) {
	p.cpuUsage.Set(percent)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Ensure Prometheus implements core.Metrics
var _ core.Metrics = (*Prometheus)(nil)
