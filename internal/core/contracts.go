package core

import (
	"context"
	"errors"
	"path/filepath"
	"time"
)

type Mode string

const (
	ModeDryRun  Mode = "dry-run"
	ModeExecute Mode = "execute"
)

type TargetType string

const (
	TargetFile TargetType = "file"
	TargetDir  TargetType = "dir"
)

type Candidate struct {
	Root         string // absolute root that discovered this candidate
	Path         string
	Type         TargetType
	Score        int // policy priority at time of action
	SizeBytes    int64
	ModTime      time.Time
	IsSymlink    bool
	LinkTarget   string
	DeviceID     uint64
	RootDeviceID uint64 // Device ID of the scan root
	FoundAt      time.Time
}

type Decision struct {
	Allow  bool
	Reason string
	Score  int
	TTL    time.Duration
}

type SafetyVerdict struct {
	Allowed bool
	Reason  string
}

type PlanItem struct {
	Candidate Candidate
	Decision  Decision
	Safety    SafetyVerdict
}

type ActionResult struct {
	Path       string
	Type       TargetType
	Score      int // policy priority at time of action
	Mode       Mode
	Deleted    bool
	BytesFreed int64
	Reason     string
	StartedAt  time.Time
	FinishedAt time.Time
	Err        error
}

var (
	ErrNotAllowed          = errors.New("not allowed")
	ErrProtectedPath       = errors.New("protected path")
	ErrOutsideAllowedRoots = errors.New("outside allowed roots")
	ErrSymlinkEscape       = errors.New("symlink escape")
	ErrUnsafeConfig        = errors.New("unsafe config")
)

type Scanner interface {
	Scan(ctx context.Context, req ScanRequest) (<-chan Candidate, <-chan error)
}

type ScanRequest struct {
	Roots          []string
	Recursive      bool
	FollowSymlinks bool
	MaxDepth       int
	IncludeDirs    bool
	IncludeFiles   bool
}

type Policy interface {
	Evaluate(ctx context.Context, cand Candidate, env EnvSnapshot) Decision
}

type Safety interface {
	Validate(ctx context.Context, cand Candidate, cfg SafetyConfig) SafetyVerdict
}

type Planner interface {
	BuildPlan(
		ctx context.Context,
		in <-chan Candidate,
		pol Policy,
		safe Safety,
		env EnvSnapshot,
		cfg SafetyConfig,
	) ([]PlanItem, error)
}

type Deleter interface {
	Execute(ctx context.Context, item PlanItem, mode Mode) ActionResult
}

type Auditor interface {
	Record(ctx context.Context, evt AuditEvent)
}

type AuditEvent struct {
	Time   time.Time
	Level  string
	Action string
	Path   string
	Fields map[string]any
	Err    error
}

// Metrics defines the interface for collecting operational metrics.
type Metrics interface {
	// Scanning metrics
	IncFilesScanned(root string)
	IncDirsScanned(root string)
	ObserveScanDuration(root string, duration time.Duration)

	// Planning metrics
	IncPolicyDecision(reason string, allowed bool)
	IncSafetyVerdict(reason string, allowed bool)
	SetBytesEligible(bytes int64)
	SetFilesEligible(count int)

	// Execution metrics
	IncFilesDeleted(root string)
	IncDirsDeleted(root string)
	AddBytesFreed(bytes int64)
	IncDeleteErrors(reason string)

	// System metrics
	SetDiskUsage(percent float64)
	SetCPUUsage(percent float64)
}

type EnvProvider interface {
	Snapshot(ctx context.Context) (EnvSnapshot, error)
}

type EnvSnapshot struct {
	Now         time.Time
	DiskUsedPct float64
	CPUUsedPct  float64
}

type SafetyConfig struct {
	AllowedRoots         []string
	ProtectedPaths       []string
	AllowDirDelete       bool
	EnforceMountBoundary bool
}

func Normalize(p string) string {
	return filepath.Clean(p)
}
