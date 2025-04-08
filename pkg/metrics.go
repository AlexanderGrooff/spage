package pkg

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	taskExecutions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "task_executions_total",
		Help: "The total number of task executions",
	}, []string{"task", "module", "host", "run_as"})

	taskSkips = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "task_skips_total",
		Help: "The total number of skipped tasks",
	}, []string{"task", "module", "host", "run_as"})

	taskErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "task_errors_total",
		Help: "The total number of task errors",
	}, []string{"task", "module", "host", "run_as"})

	taskChanges = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "task_changes_total",
		Help: "The total number of tasks that made changes",
	}, []string{"task", "module", "host", "run_as"})

	taskDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "task_duration_seconds",
		Help:    "The duration of task executions in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"task", "module", "host", "run_as"})
)

// Inc increments a counter metric
func Inc(name string, labels map[string]string) {
	switch name {
	case "task_executions_total":
		taskExecutions.With(labels).Inc()
	case "task_skips_total":
		taskSkips.With(labels).Inc()
	case "task_errors_total":
		taskErrors.With(labels).Inc()
	case "task_changes_total":
		taskChanges.With(labels).Inc()
	}
}

// Observe records a value in a histogram metric
func Observe(name string, value float64, labels map[string]string) {
	switch name {
	case "task_duration_seconds":
		taskDuration.With(labels).Observe(value)
	}
}
