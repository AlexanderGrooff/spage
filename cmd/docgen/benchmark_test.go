package main

import (
	"os"
	"os/exec"
	"testing"
)

var (
	inventory = "test_artifacts/inventory.yaml"
)

func cleanup() {
	_ = os.Remove("/tmp/spage")
}

func BenchmarkFilePlaybook(b *testing.B) {
	playbook := "test_artifacts/file_playbook.yaml"
	_ = exec.Command("spage", "generate", playbook).Run()
	_ = exec.Command("go", "build", "generated_tasks.go").Run()

	b.Run("ansible", func(b *testing.B) {
		for b.Loop() {
			_ = exec.Command("ansible-playbook", playbook, "--inventory", inventory, "--limit", "localhost").Run()
		}
	})
	cleanup()
	b.Run("spage_run", func(b *testing.B) {
		for b.Loop() {
			_ = exec.Command("spage", "run", playbook, "--inventory", inventory, "--limit", "localhost").Run()
		}
	})
	cleanup()
	b.Run("go_run_generated_tasks", func(b *testing.B) {
		for b.Loop() {
			_ = exec.Command("go", "run", "generated_tasks.go", "--inventory", inventory, "--limit", "localhost").Run()
		}
	})
	cleanup()
	b.Run("compiled_tasks", func(b *testing.B) {
		for b.Loop() {
			_ = exec.Command("generated_tasks", "--inventory", inventory, "--limit", "localhost").Run()
		}
	})
	cleanup()
	_ = os.Remove("generated_tasks.go")
	_ = os.Remove("generated_tasks")
}
