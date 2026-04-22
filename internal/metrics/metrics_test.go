package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCollectorEmitsPrometheusText(t *testing.T) {
	c := New(func() int { return 3 }, func() int { return 1 })
	c.ObserveTaskStarted()
	c.ObserveTaskResult(true, 2*time.Second)
	c.ObserveVMBoot(4 * time.Second)
	c.ObserveBytesStreamed(1024)

	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))

	body := w.Body.String()
	for _, want := range []string{
		"orchestrator_up 1",
		"orchestrator_tasks_total 1",
		"orchestrator_tasks_success_total 1",
		"orchestrator_vms_running 3",
		"orchestrator_tasks_running 1",
		"orchestrator_stream_bytes_total 1024",
		"orchestrator_vm_boot_seconds_bucket",
		"orchestrator_task_duration_seconds_sum 2",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in /metrics output:\n%s", want, body)
		}
	}
}
