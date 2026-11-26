// Package logkey names every structured log attribute key, so a typo fails to compile instead of breaking a log search.
package logkey

// Keys shared by several components.
const (
	Err        = "err"
	JobID      = "job_id"
	Repo       = "repo"
	RunnerID   = "runner_id"
	RunnerName = "runner_name"
	Labels     = "labels"
	Reason     = "reason"
	URL        = "url"
	Timeout    = "timeout"
	Limit      = "limit"
	Instance   = "instance"
)

// Keys for process, server and component lifecycle.
const (
	Version   = "version"
	Component = "component"
	Server    = "server"
	Addr      = "addr"
)

// Keys for webhook deliveries.
const (
	DeliveryID = "delivery_id"
	Event      = "event"
	Action     = "action"
	Conclusion = "conclusion"
)

// Keys for provisioning and the job state machine.
const (
	TrackedJobID = "tracked_job_id"
	From         = "from"
	To           = "to"
	Attempt      = "attempt"
	Wait         = "wait"
	Elapsed      = "elapsed"
	Workers      = "workers"
	QueueSize    = "queue_size"
	Op           = "op"
	Stage        = "stage"
	PoolSize     = "pool_size"
	PoolMax      = "pool_max"
	IdleTimeout  = "idle_timeout"
	Repos        = "repos"
)

// Keys for worker containers.
const (
	ContainerID = "container_id"
	Image       = "image"
	Network     = "network"
	ExitCode    = "exit_code"
)

// Keys for the reaper.
const (
	Age       = "age"
	JobName   = "job_name"
	Missed    = "missed"
	StaleFor  = "stale_for"
	Interval  = "interval"
	MaxAge    = "max_age"
	HealthURL = "health_url"
)

// Keys for setwebhook.
const (
	Old = "old"
	New = "new"
)

// Keys for the benchmark harness.
const (
	Workflow  = "workflow"
	Runner    = "runner"
	RunID     = "run_id"
	Iteration = "iteration"
	Total     = "total"
	Progress  = "progress"
)

// Keys for redacted log values.
const (
	Encoded = "encoded"
)
