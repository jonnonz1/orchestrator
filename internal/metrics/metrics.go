// Package metrics exposes a minimal Prometheus-compatible text-format endpoint.
//
// We deliberately avoid pulling in prometheus/client_golang to keep the
// dependency footprint small. The metrics we emit are a tiny fixed set;
// the Prometheus text format is simple enough to generate by hand and
// every scraper understands it.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Collector is the set of exported metrics.
type Collector struct {
	mu sync.RWMutex

	// Counters (monotonic)
	tasksTotal    atomic.Int64
	tasksSuccess  atomic.Int64
	tasksFailed   atomic.Int64
	vmsCreated    atomic.Int64
	vmsDestroyed  atomic.Int64
	bytesStreamed atomic.Int64

	// Gauges (sampled live via callbacks to avoid state drift)
	vmsRunning   func() int
	tasksRunning func() int

	// Histograms (fixed-bucket cumulative counters).
	// Seconds for both.
	bootDuration buckets
	taskDuration buckets

	startedAt time.Time
}

type buckets struct {
	mu     sync.Mutex
	edges  []float64 // strictly increasing upper bounds
	counts []int64   // cumulative per bucket + one for +Inf at the end
	sum    float64
	count  int64
}

func newBuckets(edges []float64) buckets {
	return buckets{
		edges:  edges,
		counts: make([]int64, len(edges)+1),
	}
}

func (b *buckets) observe(v float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.count++
	b.sum += v
	for i, e := range b.edges {
		if v <= e {
			b.counts[i]++
		}
	}
	b.counts[len(b.edges)]++ // +Inf bucket
}

// New creates a Collector. vmsRunning and tasksRunning are called on each
// scrape to sample live gauges.
func New(vmsRunning, tasksRunning func() int) *Collector {
	return &Collector{
		vmsRunning:   vmsRunning,
		tasksRunning: tasksRunning,
		bootDuration: newBuckets([]float64{0.5, 1, 2, 3, 5, 8, 13, 20, 30}),
		taskDuration: newBuckets([]float64{5, 10, 20, 30, 60, 120, 300, 600, 1800}),
		startedAt:    time.Now(),
	}
}

// ObserveTaskStarted increments tasks_total.
func (c *Collector) ObserveTaskStarted() { c.tasksTotal.Add(1) }

// ObserveTaskResult records a completed task.
func (c *Collector) ObserveTaskResult(success bool, duration time.Duration) {
	if success {
		c.tasksSuccess.Add(1)
	} else {
		c.tasksFailed.Add(1)
	}
	c.taskDuration.observe(duration.Seconds())
}

// ObserveVMBoot records a VM boot duration.
func (c *Collector) ObserveVMBoot(duration time.Duration) {
	c.vmsCreated.Add(1)
	c.bootDuration.observe(duration.Seconds())
}

// ObserveVMDestroyed increments vms_destroyed.
func (c *Collector) ObserveVMDestroyed() { c.vmsDestroyed.Add(1) }

// ObserveBytesStreamed adds n bytes to the stream counter.
func (c *Collector) ObserveBytesStreamed(n int) { c.bytesStreamed.Add(int64(n)) }

// Handler returns the HTTP handler for /metrics in Prometheus text format.
func (c *Collector) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		c.write(w)
	}
}

func (c *Collector) write(w http.ResponseWriter) {
	fmt.Fprintln(w, "# HELP orchestrator_up 1 if the orchestrator is running")
	fmt.Fprintln(w, "# TYPE orchestrator_up gauge")
	fmt.Fprintln(w, "orchestrator_up 1")

	fmt.Fprintln(w, "# HELP orchestrator_uptime_seconds Seconds since process start")
	fmt.Fprintln(w, "# TYPE orchestrator_uptime_seconds gauge")
	fmt.Fprintf(w, "orchestrator_uptime_seconds %d\n", int64(time.Since(c.startedAt).Seconds()))

	writeCounter(w, "orchestrator_tasks_total", "Total tasks created", c.tasksTotal.Load())
	writeCounter(w, "orchestrator_tasks_success_total", "Tasks that finished with exit code 0", c.tasksSuccess.Load())
	writeCounter(w, "orchestrator_tasks_failed_total", "Tasks that failed or timed out", c.tasksFailed.Load())
	writeCounter(w, "orchestrator_vms_created_total", "VMs created", c.vmsCreated.Load())
	writeCounter(w, "orchestrator_vms_destroyed_total", "VMs destroyed", c.vmsDestroyed.Load())
	writeCounter(w, "orchestrator_stream_bytes_total", "Bytes streamed from guest to host", c.bytesStreamed.Load())

	if c.vmsRunning != nil {
		writeGauge(w, "orchestrator_vms_running", "Currently running VMs", int64(c.vmsRunning()))
	}
	if c.tasksRunning != nil {
		writeGauge(w, "orchestrator_tasks_running", "Currently running tasks", int64(c.tasksRunning()))
	}

	writeHistogram(w, "orchestrator_vm_boot_seconds", "VM cold boot duration", &c.bootDuration)
	writeHistogram(w, "orchestrator_task_duration_seconds", "End-to-end task duration", &c.taskDuration)
}

func writeCounter(w http.ResponseWriter, name, help string, v int64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, v)
}

func writeGauge(w http.ResponseWriter, name, help string, v int64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, v)
}

func writeHistogram(w http.ResponseWriter, name, help string, b *buckets) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", name, help, name)
	edges := append([]float64(nil), b.edges...)
	sort.Float64s(edges)
	for i, e := range edges {
		fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", name, e, b.counts[i])
	}
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, b.counts[len(edges)])
	fmt.Fprintf(w, "%s_sum %g\n", name, b.sum)
	fmt.Fprintf(w, "%s_count %d\n", name, b.count)
}
