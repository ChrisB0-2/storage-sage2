# Storage-Sage System Architecture

Production engineering documentation for the Storage-Sage distributed cleanup system.

## Table of Contents
1. [System Context Diagram](#1-system-context-diagram)
2. [Internal Component Architecture](#2-internal-component-architecture)
3. [Data Flow Diagram](#3-data-flow-diagram)
4. [Observability Architecture](#4-observability-architecture)
5. [Execution Pipeline Detail](#5-execution-pipeline-detail)
6. [Legend](#6-legend)

---

## 1. System Context Diagram

High-level view showing all system boundaries, actors, and external integrations.

```mermaid
flowchart TB
    subgraph HUMAN["👤 HUMAN LAYER"]
        direction LR
        OPS["Operations Engineer"]
        SRE["SRE / On-Call"]
        AUDIT_REVIEWER["Compliance Auditor"]
    end

    subgraph ACCESS["🔒 ACCESS & CONTAINMENT LAYER"]
        direction LR
        PROXY["Reverse Proxy<br/>(nginx/traefik)"]
        TLS["TLS Termination"]
        AUTH["Authentication<br/>(future: OIDC/mTLS)"]
    end

    subgraph CONTROL["⚙️ CONTROL & EXECUTION LAYER"]
        direction TB
        subgraph STORAGE_SAGE["Storage-Sage Process"]
            direction TB
            DAEMON["Daemon Controller<br/>:8080"]
            METRICS_EP["Metrics Endpoint<br/>:9090"]
            WEB_UI["Embedded Web UI"]
            CORE["Core Pipeline"]
        end

        subgraph STORAGE["Persistent Storage"]
            AUDIT_DB[("SQLite Audit DB<br/>audit.db")]
            AUDIT_JSONL[("JSONL Audit Log<br/>audit.jsonl")]
        end

        FS[("Target Filesystem<br/>/data, /tmp, etc.")]
    end

    subgraph OBSERVABILITY["📊 OBSERVABILITY LAYER"]
        direction TB
        PROMETHEUS[("Prometheus<br/>:9090<br/>TSDB")]
        LOKI[("Loki<br/>:3100<br/>Log Store")]
        GRAFANA["Grafana<br/>:3000"]
        ALERTMANAGER["Alertmanager<br/>(optional)"]
    end

    subgraph EXTERNAL["🌐 EXTERNAL INTEGRATIONS"]
        WEBHOOK["Webhook Endpoints<br/>(Slack, PagerDuty)"]
    end

    %% Human interactions
    OPS -->|"HTTP/S UI"| PROXY
    SRE -->|"HTTP/S API"| PROXY
    SRE -->|"View Dashboards"| GRAFANA
    AUDIT_REVIEWER -->|"Query Audit Logs"| GRAFANA

    %% Access layer routing
    PROXY -->|"TLS"| TLS
    TLS -->|"Auth Check"| AUTH
    AUTH -->|":8080"| DAEMON
    AUTH -->|":3000"| GRAFANA

    %% Storage-Sage internal flows
    DAEMON --> WEB_UI
    DAEMON --> CORE
    CORE -->|"Scan/Delete"| FS
    CORE -->|"Write Records"| AUDIT_DB
    CORE -->|"Append Logs"| AUDIT_JSONL
    DAEMON --> METRICS_EP

    %% Observability flows
    PROMETHEUS -->|"Scrape /metrics<br/>every 10s"| METRICS_EP
    CORE -->|"Push Logs<br/>batch/5s"| LOKI
    GRAFANA -->|"PromQL"| PROMETHEUS
    GRAFANA -->|"LogQL"| LOKI
    PROMETHEUS -->|"Alerts"| ALERTMANAGER
    ALERTMANAGER -->|"Notify"| WEBHOOK

    %% External notifications
    CORE -->|"Event Webhooks"| WEBHOOK

    %% Styling
    classDef human fill:#e1f5fe,stroke:#01579b
    classDef access fill:#fff3e0,stroke:#e65100
    classDef control fill:#e8f5e9,stroke:#2e7d32
    classDef observe fill:#f3e5f5,stroke:#7b1fa2
    classDef external fill:#fce4ec,stroke:#c2185b
    classDef storage fill:#fff8e1,stroke:#f57f17

    class OPS,SRE,AUDIT_REVIEWER human
    class PROXY,TLS,AUTH access
    class DAEMON,METRICS_EP,WEB_UI,CORE control
    class PROMETHEUS,LOKI,GRAFANA,ALERTMANAGER observe
    class WEBHOOK external
    class AUDIT_DB,AUDIT_JSONL,FS storage
```

---

## 2. Internal Component Architecture

Detailed view of Storage-Sage internal subsystems and their relationships.

```mermaid
flowchart TB
    subgraph ENTRY["Entry Points"]
        CLI["CLI Parser<br/>cmd/storage-sage/main.go"]
        HTTP_TRIGGER["HTTP /trigger"]
        SCHEDULER["Interval Scheduler<br/>time.Ticker"]
    end

    subgraph DAEMON_LAYER["Daemon Layer"]
        DAEMON_CTRL["Daemon Controller<br/>internal/daemon/daemon.go"]
        STATE_MACHINE["State Machine<br/>Starting→Ready→Running→Stopping"]
        HTTP_SERVER["HTTP Server<br/>health, ready, status, API"]
        SIGNAL_HANDLER["Signal Handler<br/>SIGINT, SIGTERM"]
    end

    subgraph ORCHESTRATION["Orchestration Layer"]
        RUN_CORE["runCore()<br/>Pipeline Orchestrator"]
        CONFIG["Config Loader<br/>internal/config/"]
        ENV_SNAPSHOT["Environment Snapshot<br/>disk usage, time"]
    end

    subgraph PIPELINE["Core Pipeline"]
        direction LR
        SCANNER["Scanner<br/>internal/scanner/"]
        PLANNER["Planner<br/>internal/planner/"]
        EXECUTOR["Executor<br/>internal/executor/"]
    end

    subgraph POLICY_ENGINE["Policy Engine"]
        AGE_POLICY["AgePolicy<br/>min_age_days"]
        SIZE_POLICY["SizePolicy<br/>min_size_mb"]
        EXT_POLICY["ExtensionPolicy<br/>.tmp, .log"]
        EXCL_POLICY["ExclusionPolicy<br/>glob patterns"]
        COMPOSITE["CompositePolicy<br/>AND/OR logic"]
    end

    subgraph SAFETY_ENGINE["Safety Engine"]
        TYPE_GATE["Type Gate<br/>dir delete permission"]
        PROTECTED["Protected Paths<br/>/etc, /usr, /boot"]
        ALLOWED_ROOTS["Allowed Roots<br/>containment check"]
        ANCESTOR_SYM["Ancestor Symlink<br/>escape detection"]
        SYMLINK_ESC["Symlink Escape<br/>target validation"]
    end

    subgraph SUPPORT["Support Systems"]
        METRICS["Metrics Collector<br/>internal/metrics/"]
        LOGGER["Structured Logger<br/>internal/logger/"]
        LOKI_SHIP["Loki Shipper<br/>async batch"]
        AUDITOR["Auditor<br/>internal/auditor/"]
        NOTIFIER["Notifier<br/>webhooks"]
    end

    subgraph OUTPUTS["Output Sinks"]
        STDOUT["stdout/stderr"]
        AUDIT_FILE[("Audit Files")]
        METRICS_HTTP["/metrics HTTP"]
        LOKI_API["Loki Push API"]
        WEBHOOK_EP["Webhook Endpoints"]
    end

    %% Entry flows
    CLI --> DAEMON_CTRL
    CLI -->|"one-shot"| RUN_CORE
    HTTP_TRIGGER --> DAEMON_CTRL
    SCHEDULER --> DAEMON_CTRL

    %% Daemon layer
    DAEMON_CTRL --> STATE_MACHINE
    DAEMON_CTRL --> HTTP_SERVER
    DAEMON_CTRL --> SIGNAL_HANDLER
    DAEMON_CTRL -->|"runFunc"| RUN_CORE

    %% Orchestration
    RUN_CORE --> CONFIG
    RUN_CORE --> ENV_SNAPSHOT
    RUN_CORE --> SCANNER
    CONFIG --> COMPOSITE
    CONFIG --> SAFETY_ENGINE

    %% Pipeline flow
    SCANNER -->|"chan Candidate"| PLANNER
    PLANNER -->|"[]PlanItem"| EXECUTOR

    %% Policy composition
    AGE_POLICY --> COMPOSITE
    SIZE_POLICY --> COMPOSITE
    EXT_POLICY --> COMPOSITE
    EXCL_POLICY --> COMPOSITE
    COMPOSITE -->|"Evaluate()"| PLANNER

    %% Safety validation
    TYPE_GATE --> PLANNER
    PROTECTED --> PLANNER
    ALLOWED_ROOTS --> PLANNER
    ANCESTOR_SYM --> PLANNER
    SYMLINK_ESC --> PLANNER
    SAFETY_ENGINE -->|"Re-validate"| EXECUTOR

    %% Support system connections
    SCANNER --> METRICS
    PLANNER --> METRICS
    EXECUTOR --> METRICS
    PLANNER --> AUDITOR
    EXECUTOR --> AUDITOR
    RUN_CORE --> LOGGER
    LOGGER --> LOKI_SHIP
    EXECUTOR --> NOTIFIER

    %% Output flows
    LOGGER --> STDOUT
    AUDITOR --> AUDIT_FILE
    METRICS --> METRICS_HTTP
    LOKI_SHIP --> LOKI_API
    NOTIFIER --> WEBHOOK_EP

    %% Styling
    classDef entry fill:#e3f2fd,stroke:#1565c0
    classDef daemon fill:#fff3e0,stroke:#ef6c00
    classDef orch fill:#e8f5e9,stroke:#2e7d32
    classDef pipeline fill:#f3e5f5,stroke:#7b1fa2
    classDef policy fill:#fce4ec,stroke:#c2185b
    classDef safety fill:#ffebee,stroke:#c62828
    classDef support fill:#e0f2f1,stroke:#00695c
    classDef output fill:#eceff1,stroke:#37474f

    class CLI,HTTP_TRIGGER,SCHEDULER entry
    class DAEMON_CTRL,STATE_MACHINE,HTTP_SERVER,SIGNAL_HANDLER daemon
    class RUN_CORE,CONFIG,ENV_SNAPSHOT orch
    class SCANNER,PLANNER,EXECUTOR pipeline
    class AGE_POLICY,SIZE_POLICY,EXT_POLICY,EXCL_POLICY,COMPOSITE policy
    class TYPE_GATE,PROTECTED,ALLOWED_ROOTS,ANCESTOR_SYM,SYMLINK_ESC safety
    class METRICS,LOGGER,LOKI_SHIP,AUDITOR,NOTIFIER support
    class STDOUT,AUDIT_FILE,METRICS_HTTP,LOKI_API,WEBHOOK_EP output
```

---

## 3. Data Flow Diagram

Shows data transformation through the system with explicit types.

```mermaid
flowchart LR
    subgraph INPUT["Input"]
        FS_ENTRIES["Filesystem<br/>Entries"]
        CONFIG_YAML["config.yaml"]
        CLI_FLAGS["CLI Flags"]
    end

    subgraph SCANNING["Scanning Phase"]
        WALK["filepath.WalkDir()"]
        CANDIDATE["Candidate{<br/>Path, Size, ModTime,<br/>IsDir, IsSymlink,<br/>SymlinkTarget, DeviceID}"]
    end

    subgraph PLANNING["Planning Phase"]
        POLICY_EVAL["Policy.Evaluate()"]
        DECISION["Decision{<br/>Allow, Reason, Score}"]
        SAFETY_VAL["Safety.Validate()"]
        VERDICT["SafetyVerdict{<br/>Allowed, Reason}"]
        PLAN_ITEM["PlanItem{<br/>Candidate,<br/>Decision,<br/>SafetyVerdict}"]
    end

    subgraph EXECUTION["Execution Phase"]
        GATE_CHECK["5-Gate Validation"]
        OS_REMOVE["os.Remove()<br/>os.RemoveAll()"]
        RESULT["ActionResult{<br/>Deleted, BytesFreed,<br/>Reason, Error}"]
    end

    subgraph OUTPUT["Output"]
        METRICS_DATA["Prometheus Metrics<br/>Counters, Gauges,<br/>Histograms"]
        AUDIT_RECORD["AuditRecord{<br/>Timestamp, Action,<br/>Path, Decision,<br/>BytesFreed, Checksum}"]
        LOG_ENTRY["Log Entry{<br/>Time, Level, Msg,<br/>Fields}"]
    end

    %% Data flow
    FS_ENTRIES --> WALK
    CONFIG_YAML --> POLICY_EVAL
    CONFIG_YAML --> SAFETY_VAL
    CLI_FLAGS --> CONFIG_YAML

    WALK -->|"emit on channel"| CANDIDATE
    CANDIDATE --> POLICY_EVAL
    CANDIDATE --> SAFETY_VAL
    POLICY_EVAL --> DECISION
    SAFETY_VAL --> VERDICT
    DECISION --> PLAN_ITEM
    VERDICT --> PLAN_ITEM
    CANDIDATE --> PLAN_ITEM

    PLAN_ITEM --> GATE_CHECK
    GATE_CHECK -->|"if allowed"| OS_REMOVE
    OS_REMOVE --> RESULT
    GATE_CHECK -->|"if denied"| RESULT

    PLAN_ITEM --> AUDIT_RECORD
    RESULT --> AUDIT_RECORD
    RESULT --> METRICS_DATA
    RESULT --> LOG_ENTRY
    PLAN_ITEM --> LOG_ENTRY

    %% Styling
    classDef input fill:#e8eaf6,stroke:#3f51b5
    classDef scan fill:#e3f2fd,stroke:#1976d2
    classDef plan fill:#e8f5e9,stroke:#388e3c
    classDef exec fill:#fff3e0,stroke:#f57c00
    classDef out fill:#fce4ec,stroke:#d81b60

    class FS_ENTRIES,CONFIG_YAML,CLI_FLAGS input
    class WALK,CANDIDATE scan
    class POLICY_EVAL,DECISION,SAFETY_VAL,VERDICT,PLAN_ITEM plan
    class GATE_CHECK,OS_REMOVE,RESULT exec
    class METRICS_DATA,AUDIT_RECORD,LOG_ENTRY out
```

---

## 4. Observability Architecture

Complete observability stack with all data paths.

```mermaid
flowchart TB
    subgraph STORAGE_SAGE["Storage-Sage Process"]
        subgraph INSTRUMENTATION["Instrumentation Points"]
            SCAN_METRICS["Scanner Metrics<br/>files_scanned, dirs_scanned,<br/>scan_duration"]
            PLAN_METRICS["Planner Metrics<br/>policy_decisions,<br/>safety_verdicts,<br/>bytes_eligible"]
            EXEC_METRICS["Executor Metrics<br/>files_deleted,<br/>bytes_freed,<br/>delete_errors"]
            SYS_METRICS["System Metrics<br/>disk_usage,<br/>cpu_usage"]
        end

        PROM_CLIENT["Prometheus Client<br/>prometheus/client_golang"]
        METRICS_SERVER["HTTP Server :9090<br/>/metrics endpoint"]

        subgraph LOGGING["Logging Pipeline"]
            JSON_LOGGER["JSONLogger<br/>Structured Logs"]
            LOKI_BATCHER["Loki Batcher<br/>batch_size=100<br/>batch_wait=5s"]
            LOKI_HTTP["HTTP Client<br/>POST /loki/api/v1/push"]
        end
    end

    subgraph PROMETHEUS_STACK["Prometheus Stack"]
        PROM_SERVER["Prometheus Server<br/>:9090"]
        TSDB[("TSDB<br/>Time Series Data")]
        PROM_RULES["Recording Rules<br/>Alerting Rules"]
        SERVICE_DISC["Service Discovery<br/>static_configs"]
    end

    subgraph LOKI_STACK["Loki Stack"]
        LOKI_SERVER["Loki Server<br/>:3100"]
        LOKI_STORE[("Chunk Store<br/>Index + Chunks")]
        LOKI_QUERY["LogQL Engine"]
    end

    subgraph GRAFANA_STACK["Grafana Stack"]
        GRAFANA_SERVER["Grafana Server<br/>:3000"]
        DASHBOARDS["Provisioned Dashboards<br/>storage-sage-operations.json"]
        DATASOURCES["Datasources<br/>Prometheus + Loki"]
        ALERTING["Grafana Alerting<br/>Alert Rules"]
    end

    subgraph ALERTING_STACK["Alerting Stack"]
        ALERTMANAGER["Alertmanager<br/>(optional)"]
        ALERT_ROUTES["Alert Routes<br/>Grouping, Inhibition"]
        RECEIVERS["Receivers<br/>Slack, PagerDuty,<br/>Email, Webhook"]
    end

    subgraph HUMAN["Human Consumers"]
        DASHBOARD_VIEW["Dashboard Viewer"]
        LOG_EXPLORER["Log Explorer"]
        ALERT_RESPONDER["Alert Responder"]
    end

    %% Metrics flow
    SCAN_METRICS --> PROM_CLIENT
    PLAN_METRICS --> PROM_CLIENT
    EXEC_METRICS --> PROM_CLIENT
    SYS_METRICS --> PROM_CLIENT
    PROM_CLIENT --> METRICS_SERVER

    SERVICE_DISC -->|"discover targets"| PROM_SERVER
    PROM_SERVER -->|"GET /metrics<br/>scrape_interval=10s"| METRICS_SERVER
    PROM_SERVER --> TSDB
    PROM_SERVER --> PROM_RULES

    %% Log flow
    JSON_LOGGER --> LOKI_BATCHER
    LOKI_BATCHER -->|"batch flush"| LOKI_HTTP
    LOKI_HTTP -->|"POST<br/>{streams: [...]}"| LOKI_SERVER
    LOKI_SERVER --> LOKI_STORE
    LOKI_SERVER --> LOKI_QUERY

    %% Grafana queries
    GRAFANA_SERVER --> DATASOURCES
    DATASOURCES -->|"PromQL"| PROM_SERVER
    DATASOURCES -->|"LogQL"| LOKI_QUERY
    GRAFANA_SERVER --> DASHBOARDS
    GRAFANA_SERVER --> ALERTING

    %% Alerting flow
    PROM_RULES -->|"firing alerts"| ALERTMANAGER
    ALERTING -->|"firing alerts"| ALERTMANAGER
    ALERTMANAGER --> ALERT_ROUTES
    ALERT_ROUTES --> RECEIVERS

    %% Human interaction
    DASHBOARD_VIEW -->|"view"| DASHBOARDS
    LOG_EXPLORER -->|"query"| LOKI_QUERY
    RECEIVERS -->|"notify"| ALERT_RESPONDER

    %% Styling
    classDef sage fill:#e8f5e9,stroke:#2e7d32
    classDef prom fill:#fff3e0,stroke:#ef6c00
    classDef loki fill:#e3f2fd,stroke:#1565c0
    classDef grafana fill:#f3e5f5,stroke:#7b1fa2
    classDef alert fill:#ffebee,stroke:#c62828
    classDef human fill:#e1f5fe,stroke:#01579b

    class SCAN_METRICS,PLAN_METRICS,EXEC_METRICS,SYS_METRICS,PROM_CLIENT,METRICS_SERVER,JSON_LOGGER,LOKI_BATCHER,LOKI_HTTP sage
    class PROM_SERVER,TSDB,PROM_RULES,SERVICE_DISC prom
    class LOKI_SERVER,LOKI_STORE,LOKI_QUERY loki
    class GRAFANA_SERVER,DASHBOARDS,DATASOURCES,ALERTING grafana
    class ALERTMANAGER,ALERT_ROUTES,RECEIVERS alert
    class DASHBOARD_VIEW,LOG_EXPLORER,ALERT_RESPONDER human
```

---

## 5. Execution Pipeline Detail

Detailed view of the 5-gate execution model with TOCTOU protection.

```mermaid
flowchart TB
    subgraph INPUT["Pipeline Input"]
        PLAN["[]PlanItem<br/>sorted by path"]
    end

    subgraph GATE1["Gate 1: Policy Check"]
        P_CHECK{"PlanItem.Decision<br/>.Allow == true?"}
        P_DENY["ActionResult{<br/>Reason: policy_deny}"]
    end

    subgraph GATE2["Gate 2: Scan-Time Safety"]
        S_CHECK{"PlanItem.SafetyVerdict<br/>.Allowed == true?"}
        S_DENY["ActionResult{<br/>Reason: safety_deny_scan}"]
    end

    subgraph GATE3["Gate 3: Execute-Time Safety (TOCTOU)"]
        REVALIDATE["safety.Validate()<br/>Re-check at delete time"]
        T_CHECK{"Re-validation<br/>passed?"}
        T_DENY["ActionResult{<br/>Reason: safety_deny_execute}"]
    end

    subgraph GATE4["Gate 4: Mode Check"]
        M_CHECK{"mode ==<br/>execute?"}
        DRY_RUN["ActionResult{<br/>Reason: would_delete,<br/>Deleted: false}"]
    end

    subgraph GATE5["Gate 5: Filesystem Operation"]
        STAT_CHECK["os.Lstat()<br/>verify exists"]
        GONE_CHECK{"File still<br/>exists?"}
        ALREADY_GONE["ActionResult{<br/>Reason: already_gone}"]

        IS_DIR{"IsDir?"}
        REMOVE_FILE["os.Remove(path)"]
        REMOVE_DIR["os.RemoveAll(path)"]

        OP_CHECK{"Operation<br/>succeeded?"}
        DELETE_FAIL["ActionResult{<br/>Reason: delete_failed,<br/>Error: ...}"]
    end

    subgraph SUCCESS["Success Output"]
        SUCCESS_RESULT["ActionResult{<br/>Deleted: true,<br/>BytesFreed: N,<br/>Reason: deleted}"]
    end

    subgraph SIDE_EFFECTS["Side Effects"]
        AUDIT_LOG["Audit Record"]
        METRIC_INC["Metric Increment"]
        LOG_ENTRY["Log Entry"]
    end

    %% Main flow
    PLAN --> P_CHECK
    P_CHECK -->|"No"| P_DENY
    P_CHECK -->|"Yes"| S_CHECK

    S_CHECK -->|"No"| S_DENY
    S_CHECK -->|"Yes"| REVALIDATE

    REVALIDATE --> T_CHECK
    T_CHECK -->|"No"| T_DENY
    T_CHECK -->|"Yes"| M_CHECK

    M_CHECK -->|"dry-run"| DRY_RUN
    M_CHECK -->|"execute"| STAT_CHECK

    STAT_CHECK --> GONE_CHECK
    GONE_CHECK -->|"No"| ALREADY_GONE
    GONE_CHECK -->|"Yes"| IS_DIR

    IS_DIR -->|"No"| REMOVE_FILE
    IS_DIR -->|"Yes"| REMOVE_DIR

    REMOVE_FILE --> OP_CHECK
    REMOVE_DIR --> OP_CHECK

    OP_CHECK -->|"Error"| DELETE_FAIL
    OP_CHECK -->|"OK"| SUCCESS_RESULT

    %% Side effects from all results
    P_DENY --> AUDIT_LOG
    S_DENY --> AUDIT_LOG
    T_DENY --> AUDIT_LOG
    DRY_RUN --> AUDIT_LOG
    ALREADY_GONE --> AUDIT_LOG
    DELETE_FAIL --> AUDIT_LOG
    SUCCESS_RESULT --> AUDIT_LOG

    SUCCESS_RESULT --> METRIC_INC
    DELETE_FAIL --> METRIC_INC

    P_DENY --> LOG_ENTRY
    T_DENY --> LOG_ENTRY
    DELETE_FAIL --> LOG_ENTRY
    SUCCESS_RESULT --> LOG_ENTRY

    %% Styling
    classDef input fill:#e8eaf6,stroke:#3f51b5
    classDef gate fill:#fff8e1,stroke:#f9a825
    classDef deny fill:#ffebee,stroke:#c62828
    classDef success fill:#e8f5e9,stroke:#2e7d32
    classDef effect fill:#e3f2fd,stroke:#1565c0

    class PLAN input
    class P_CHECK,S_CHECK,T_CHECK,M_CHECK,GONE_CHECK,IS_DIR,OP_CHECK gate
    class P_DENY,S_DENY,T_DENY,DELETE_FAIL,ALREADY_GONE deny
    class SUCCESS_RESULT,DRY_RUN success
    class AUDIT_LOG,METRIC_INC,LOG_ENTRY effect
```

---

## 6. Legend

### Block Types

| Symbol | Type | Description |
|--------|------|-------------|
| Rectangle | **Action System** | Component that performs operations (Scanner, Executor) |
| Diamond | **Decision Point** | Conditional branching logic |
| Cylinder | **Data Store** | Persistent storage (SQLite, TSDB, Filesystem) |
| Rounded Rectangle | **Service** | Long-running process (Daemon, Prometheus, Grafana) |
| Parallelogram | **Data Object** | In-flight data structure (Candidate, PlanItem) |

### Arrow Types

| Arrow | Type | Description |
|-------|------|-------------|
| `-->` | **Control Flow** | Execution sequence or function call |
| `-->\|label\|` | **Data Flow** | Data transformation with type annotation |
| `-.->` | **Async/Batch** | Asynchronous or batched operation |
| `==>` | **Aggregation** | Multiple sources to single sink |

### Color Coding

| Color | Layer | Examples |
|-------|-------|----------|
| Blue (#e3f2fd) | **Input/Entry** | CLI, HTTP triggers, filesystem |
| Green (#e8f5e9) | **Processing** | Scanner, Planner, Executor |
| Orange (#fff3e0) | **Control** | Daemon, Scheduler, State Machine |
| Purple (#f3e5f5) | **Observability** | Metrics, Grafana, Dashboards |
| Red (#ffebee) | **Safety/Deny** | Safety engine, error paths |
| Yellow (#fff8e1) | **Decision** | Policy evaluation, gate checks |
| Grey (#eceff1) | **Output** | Audit logs, stdout, webhooks |

### Data Flow Paths

| Path | Protocol | Direction | Frequency |
|------|----------|-----------|-----------|
| Metrics Scrape | HTTP GET `/metrics` | Prometheus → Storage-Sage | Every 10s |
| Log Push | HTTP POST `/loki/api/v1/push` | Storage-Sage → Loki | Batch every 5s or 100 entries |
| Dashboard Query | HTTP PromQL/LogQL | Grafana → Prometheus/Loki | On refresh (30s default) |
| Webhook Notify | HTTP POST | Storage-Sage → External | On cleanup events |
| Audit Write | SQLite/File I/O | Storage-Sage → Disk | Per plan item + execution |

### System Roles

| Role | Components | Purpose |
|------|------------|---------|
| **Action** | Scanner, Executor, os.Remove | Perform filesystem operations |
| **Sensing** | Metrics Collector, Auditor | Capture system state and events |
| **Cognition** | Policy Engine, Safety Engine, Planner | Decision-making and planning |
| **Visualization** | Grafana, Web UI | Human-readable presentation |
| **Feedback** | Alertmanager, Webhooks | Closed-loop notification |

---

## Appendix: Network Ports

| Service | Port | Protocol | Purpose |
|---------|------|----------|---------|
| Storage-Sage Daemon | 8080 | HTTP | Web UI + REST API |
| Storage-Sage Metrics | 9090 (internal), 9091 (external) | HTTP | Prometheus metrics |
| Prometheus | 9090 | HTTP | Metrics storage + query |
| Loki | 3100 | HTTP | Log ingestion + query |
| Grafana | 3000 | HTTP | Visualization |

---

## Appendix: Metric Reference

### Counters (monotonically increasing)
- `storagesage_scanner_files_scanned_total{root}`
- `storagesage_scanner_dirs_scanned_total{root}`
- `storagesage_planner_policy_decisions_total{reason,allowed}`
- `storagesage_planner_safety_verdicts_total{reason,allowed}`
- `storagesage_executor_files_deleted_total{root}`
- `storagesage_executor_dirs_deleted_total{root}`
- `storagesage_executor_bytes_freed_total`
- `storagesage_executor_delete_errors_total{reason}`

### Gauges (point-in-time values)
- `storagesage_planner_bytes_eligible`
- `storagesage_planner_files_eligible`
- `storagesage_system_disk_usage_percent`
- `storagesage_system_cpu_usage_percent`

### Histograms (distributions)
- `storagesage_scanner_scan_duration_seconds{root}` (buckets: 0.1s to 100s)
