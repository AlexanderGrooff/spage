package modules

import (
	"reflect"
	"sync"
	"testing"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlockModule(t *testing.T) {
	var _ pkg.MetaModule = BlockModule{}
	blockModule := BlockModule{}

	// Create a proper MetaTask with TaskParams
	metaTask := &pkg.MetaTask{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "test-block",
			Module: "block",
		},
		Children: []pkg.GraphNode{},
	}

	// Create a proper Closure with config
	closure := &pkg.Closure{
		HostContext: &pkg.HostContext{
			Host:  &pkg.Host{Name: "testhost", IsLocal: true},
			Facts: &sync.Map{},
		},
		ExtraFacts: make(map[string]interface{}),
		Config:     &config.Config{},
	}

	// This should not panic
	results := blockModule.EvaluateExecute(metaTask, closure)

	// Consume the results to avoid goroutine leak
	for range results {
		// Just consume the results
	}
}

func TestBlockModuleWithChildren(t *testing.T) {
	blockModule := BlockModule{}

	// Create mock tasks for children
	mockTask1 := &pkg.Task{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "task1",
			Module: "command",
		},
	}

	mockTask2 := &pkg.Task{
		TaskParams: &pkg.TaskParams{
			Id:     2,
			Name:   "task2",
			Module: "command",
		},
	}

	// Create a MetaTask with children
	metaTask := &pkg.MetaTask{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "test-block",
			Module: "block",
		},
		Children: []pkg.GraphNode{mockTask1, mockTask2},
	}

	// Create a proper Closure with config
	closure := &pkg.Closure{
		HostContext: &pkg.HostContext{
			Host:  &pkg.Host{Name: "testhost", IsLocal: true},
			Facts: &sync.Map{},
		},
		ExtraFacts: make(map[string]interface{}),
		Config:     &config.Config{},
	}

	// Execute the block
	results := blockModule.EvaluateExecute(metaTask, closure)

	// Collect results
	var resultCount int
	for result := range results {
		resultCount++
		assert.NotNil(t, result.Task, "Result should have a task")
		assert.NotNil(t, result.Closure, "Result should have a closure")
	}

	// Should have 1 result for the block (aggregated)
	assert.Equal(t, 1, resultCount, "Should have 1 result for the block")
}

func TestBlockModuleWithRescue(t *testing.T) {
	blockModule := BlockModule{}

	// Create mock tasks for children (these will fail)
	mockFailingTask := &pkg.Task{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "failing-task",
			Module: "command",
		},
	}

	// Create a MetaTask with children (rescue not supported in current implementation)
	metaTask := &pkg.MetaTask{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "test-block",
			Module: "block",
		},
		Children: []pkg.GraphNode{mockFailingTask},
	}

	// Create a proper Closure with config
	closure := &pkg.Closure{
		HostContext: &pkg.HostContext{
			Host:  &pkg.Host{Name: "testhost", IsLocal: true},
			Facts: &sync.Map{},
		},
		ExtraFacts: make(map[string]interface{}),
		Config:     &config.Config{},
	}

	// Execute the block
	results := blockModule.EvaluateExecute(metaTask, closure)

	// Collect results
	var resultCount int
	for result := range results {
		resultCount++
		assert.NotNil(t, result.Task, "Result should have a task")
		assert.NotNil(t, result.Closure, "Result should have a closure")
	}

	// Should have 1 result from children (rescue only runs if block fails)
	assert.Equal(t, 1, resultCount, "Should have 1 result (1 child, rescue only runs on failure)")
}

func TestBlockModuleWithAlways(t *testing.T) {
	blockModule := BlockModule{}

	// Create mock tasks for children
	mockTask := &pkg.Task{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "task",
			Module: "set_fact",
			Params: pkg.ModuleInput{Actual: SetFactInput{Facts: map[string]interface{}{"test_var": "test_value"}}},
		},
	}

	// Create mock task for always
	mockAlwaysTask := &pkg.Task{
		TaskParams: &pkg.TaskParams{
			Id:     2,
			Name:   "always task",
			Module: "set_fact",
			Params: pkg.ModuleInput{Actual: SetFactInput{Facts: map[string]interface{}{"always_var": "always_value"}}},
		},
	}

	// Create a MetaTask with children and always
	metaTask := &pkg.MetaTask{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "test-block",
			Module: "block",
		},
		Children: []pkg.GraphNode{mockTask},
		Always:   []pkg.GraphNode{mockAlwaysTask},
	}

	// Create a proper Closure with config
	closure := &pkg.Closure{
		HostContext: &pkg.HostContext{
			Host:  &pkg.Host{Name: "testhost", IsLocal: true},
			Facts: &sync.Map{},
		},
		ExtraFacts: make(map[string]interface{}),
		Config:     &config.Config{},
	}

	// Execute the block
	results := blockModule.EvaluateExecute(metaTask, closure)

	// Collect results
	var resultCount int
	for result := range results {
		resultCount++
		assert.NotNil(t, result.Task, "Result should have a task")
		assert.NotNil(t, result.Closure, "Result should have a closure")
	}

	// Should have 1 result for the block (aggregated)
	assert.Equal(t, 1, resultCount, "Should have 1 result for the block")
}

func TestBlockModuleWithRescueAndAlways(t *testing.T) {
	blockModule := BlockModule{}

	// Create mock tasks for children
	mockTask := &pkg.Task{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "task",
			Module: "set_fact",
			Params: pkg.ModuleInput{Actual: SetFactInput{Facts: map[string]interface{}{"test_var": "test_value"}}},
		},
	}

	// Create mock tasks for rescue and always
	mockRescueTask := &pkg.Task{
		TaskParams: &pkg.TaskParams{
			Id:     2,
			Name:   "rescue task",
			Module: "set_fact",
			Params: pkg.ModuleInput{Actual: SetFactInput{Facts: map[string]interface{}{"rescue_var": "rescue_value"}}},
		},
	}
	mockAlwaysTask := &pkg.Task{
		TaskParams: &pkg.TaskParams{
			Id:     3,
			Name:   "always task",
			Module: "set_fact",
			Params: pkg.ModuleInput{Actual: SetFactInput{Facts: map[string]interface{}{"always_var": "always_value"}}},
		},
	}

	// Create a MetaTask with children, rescue, and always
	metaTask := &pkg.MetaTask{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "test-block",
			Module: "block",
		},
		Children: []pkg.GraphNode{mockTask},
		Rescue:   []pkg.GraphNode{mockRescueTask},
		Always:   []pkg.GraphNode{mockAlwaysTask},
	}

	// Create a proper Closure with config
	closure := &pkg.Closure{
		HostContext: &pkg.HostContext{
			Host:  &pkg.Host{Name: "testhost", IsLocal: true},
			Facts: &sync.Map{},
		},
		ExtraFacts: make(map[string]interface{}),
		Config:     &config.Config{},
	}

	// Execute the block
	results := blockModule.EvaluateExecute(metaTask, closure)

	// Collect results
	var resultCount int
	for result := range results {
		resultCount++
		assert.NotNil(t, result.Task, "Result should have a task")
		assert.NotNil(t, result.Closure, "Result should have a closure")
	}

	// Should have 1 result for the block (aggregated)
	assert.Equal(t, 1, resultCount, "Should have 1 result for the block")
}

func TestBlockModuleEmptyChildren(t *testing.T) {
	blockModule := BlockModule{}

	// Create a MetaTask with no children
	metaTask := &pkg.MetaTask{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "test-block",
			Module: "block",
		},
		Children: []pkg.GraphNode{},
	}

	// Create a proper Closure with config
	closure := &pkg.Closure{
		HostContext: &pkg.HostContext{
			Host:  &pkg.Host{Name: "testhost", IsLocal: true},
			Facts: &sync.Map{},
		},
		ExtraFacts: make(map[string]interface{}),
		Config:     &config.Config{},
	}

	// Execute the block
	results := blockModule.EvaluateExecute(metaTask, closure)

	// Collect results
	var resultCount int
	for result := range results {
		resultCount++
		assert.NotNil(t, result.Task, "Result should have a task")
		assert.NotNil(t, result.Closure, "Result should have a closure")
	}

	// Should have 1 result for the block (even with empty children)
	assert.Equal(t, 1, resultCount, "Should have 1 result for the block")
}

func TestBlockModuleRevert(t *testing.T) {
	blockModule := BlockModule{}

	// Create mock tasks for children
	mockTask := &pkg.Task{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "task",
			Module: "command",
		},
	}

	// Create a MetaTask with children
	metaTask := &pkg.MetaTask{
		TaskParams: &pkg.TaskParams{
			Id:     1,
			Name:   "test-block",
			Module: "block",
		},
		Children: []pkg.GraphNode{mockTask},
	}

	// Create a proper Closure with config
	closure := &pkg.Closure{
		HostContext: &pkg.HostContext{
			Host:  &pkg.Host{Name: "testhost", IsLocal: true},
			Facts: &sync.Map{},
		},
		ExtraFacts: make(map[string]interface{}),
		Config:     &config.Config{},
	}

	// Execute the block revert
	results := blockModule.EvaluateRevert(metaTask, closure)

	// Collect results
	var resultCount int
	for result := range results {
		resultCount++
		assert.NotNil(t, result.Task, "Result should have a task")
		assert.NotNil(t, result.Closure, "Result should have a closure")
	}

	// Should have 1 result for the child task
	assert.Equal(t, 1, resultCount, "Should have 1 result for 1 child task in revert")
}

func TestBlockModuleInputType(t *testing.T) {
	blockModule := BlockModule{}

	// Test InputType method
	inputType := blockModule.InputType()
	require.NotNil(t, inputType, "InputType should not be nil")

	// Should return BlockInput type
	expectedType := reflect.TypeOf(BlockInput{})
	assert.Equal(t, expectedType, inputType, "InputType should return BlockInput type")
}

func TestBlockModuleOutputType(t *testing.T) {
	blockModule := BlockModule{}

	// Test OutputType method
	outputType := blockModule.OutputType()
	require.NotNil(t, outputType, "OutputType should not be nil")

	// Should return BlockOutput type
	expectedType := reflect.TypeOf(BlockOutput{})
	assert.Equal(t, expectedType, outputType, "OutputType should return BlockOutput type")
}
