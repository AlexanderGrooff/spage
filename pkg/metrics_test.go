package pkg

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
)

func TestMetrics(t *testing.T) {
	// Create a new test registry
	registry := prometheus.NewRegistry()

	// Create metrics with the test registry
	taskExecutions := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "task_executions_total",
		Help: "The total number of task executions",
	}, []string{"task", "module", "host", "run_as"})
	registry.MustRegister(taskExecutions)

	taskSkips := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "task_skips_total",
		Help: "The total number of skipped tasks",
	}, []string{"task", "module", "host", "run_as"})
	registry.MustRegister(taskSkips)

	taskDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "task_duration_seconds",
		Help:    "The duration of task executions in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"task", "module", "host", "run_as"})
	registry.MustRegister(taskDuration)

	// Test cases
	labels := map[string]string{
		"task":   "test_task",
		"module": "test_module",
		"host":   "test_host",
		"run_as": "test_user",
	}

	// Test task executions counter
	t.Run("increment task executions", func(t *testing.T) {
		taskExecutions.With(labels).Inc()
		
		counter, err := taskExecutions.GetMetricWith(labels)
		assert.NoError(t, err)
		
		value := getCounterValue(counter)
		assert.Equal(t, float64(1), value)
	})

	// Test task skips counter
	t.Run("increment task skips", func(t *testing.T) {
		taskSkips.With(labels).Inc()
		
		counter, err := taskSkips.GetMetricWith(labels)
		assert.NoError(t, err)
		
		value := getCounterValue(counter)
		assert.Equal(t, float64(1), value)
	})

	// Test task duration histogram
	t.Run("observe task duration", func(t *testing.T) {
		taskDuration.With(labels).Observe(1.5)
		
		histogram, err := taskDuration.GetMetricWith(labels)
		assert.NoError(t, err)
		
		count := getHistogramCount(histogram)
		assert.Equal(t, uint64(1), count)
	})
}

// Helper function to get counter value
func getCounterValue(counter prometheus.Counter) float64 {
	var metric dto.Metric
	counter.Write(&metric)
	return *metric.Counter.Value
}

// Helper function to get histogram count
func getHistogramCount(histogram prometheus.Observer) uint64 {
	var metric dto.Metric
	if h, ok := histogram.(prometheus.Metric); ok {
		h.Write(&metric)
		return *metric.Histogram.SampleCount
	}
	return 0
}
