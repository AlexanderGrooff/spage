package main

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

var (
	inventory = "test_artifacts/inventory.yaml"
)

// PlaybookTest represents a playbook test configuration
type PlaybookTest struct {
	Name     string
	Playbook string
}

var playbooks = []PlaybookTest{
	{"File", "test_artifacts/file_playbook.yaml"},
	{"Command", "test_artifacts/command_playbook.yaml"},
	{"Jinja", "test_artifacts/jinja_tests_playbook.yaml"},
}

var bashScripts = []PlaybookTest{
	{"File", "test_artifacts/file_script.sh"},
	{"Command", "test_artifacts/command_script.sh"},
	{"Jinja", "test_artifacts/jinja_script.sh"},
}

func cleanup() {
	_ = os.RemoveAll("/tmp/spage")
	_ = os.MkdirAll("/tmp/spage", 0755)
}

func setupPlaybook(playbook string) error {
	// Use the spage binary from ~/scripts/
	if err := exec.Command("spage", "generate", playbook).Run(); err != nil {
		return fmt.Errorf("failed to generate playbook %s: %w", playbook, err)
	}
	// Build the generated code
	if err := exec.Command("go", "build", "-o", "generated_tasks", "generated_tasks.go").Run(); err != nil {
		return fmt.Errorf("failed to build generated tasks for %s: %w", playbook, err)
	}
	return nil
}

// BenchmarkPlaybooks runs benchmarks for all playbooks
func BenchmarkPlaybooks(b *testing.B) {
	for _, pb := range playbooks {
		b.Run(pb.Name, func(b *testing.B) {
			benchmarkPlaybook(b, pb.Playbook)
		})
	}
}

// BenchmarkBashScripts runs benchmarks for bash scripts only
func BenchmarkBashScripts(b *testing.B) {
	for _, bs := range bashScripts {
		b.Run(bs.Name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cleanup()
				cmd := exec.Command("bash", bs.Playbook)
				if err := cmd.Run(); err != nil {
					b.Logf("Bash run %d failed: %v", i, err)
				}
			}
		})
	}
}

func benchmarkPlaybook(b *testing.B, playbook string) {
	b.Run("ansible", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cleanup()
			cmd := exec.Command("ansible-playbook", playbook, "--inventory", inventory, "--limit", "localhost")
			if err := cmd.Run(); err != nil {
				b.Logf("Ansible run %d failed: %v", i, err)
			}
		}
	})

	// Find corresponding bash script
	bashScript := ""
	for _, bs := range bashScripts {
		for _, pb := range playbooks {
			if pb.Playbook == playbook && pb.Name == bs.Name {
				bashScript = bs.Playbook
				break
			}
		}
		if bashScript != "" {
			break
		}
	}

	if bashScript != "" {
		b.Run("bash", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cleanup()
				cmd := exec.Command("bash", bashScript)
				if err := cmd.Run(); err != nil {
					b.Logf("Bash run %d failed: %v", i, err)
				}
			}
		})
	}

	b.Run("spage_run", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cleanup()
			cmd := exec.Command("spage", "run", playbook, "--inventory", inventory, "--limit", "localhost", "--config", "test_artifacts/spage.yaml")
			if err := cmd.Run(); err != nil {
				b.Logf("Spage run %d failed: %v", i, err)
			}
		}
	})

	b.Run("spage_run_temporal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cleanup()
			cmd := exec.Command("spage", "run", playbook, "--inventory", inventory, "--limit", "localhost", "--config", "test_artifacts/spage.yaml")
			cmd.Env = append(os.Environ(), "SPAGE_EXECUTOR=temporal")
			if err := cmd.Run(); err != nil {
				b.Logf("Spage run temporal %d failed: %v", i, err)
			}
		}
	})

	b.Run("go_run_generated", func(b *testing.B) {
		// Setup once BEFORE starting the timer
		if err := setupPlaybook(playbook); err != nil {
			b.Fatalf("Setup failed for %s: %v", playbook, err)
		}
		defer func() {
			_ = os.Remove("generated_tasks.go")
		}()

		// Reset timer to exclude setup time
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			cleanup()
			cmd := exec.Command("go", "run", "generated_tasks.go", "--inventory", inventory, "--limit", "localhost", "--config", "test_artifacts/spage.yaml")
			if err := cmd.Run(); err != nil {
				b.Logf("Go run generated %d failed: %v", i, err)
			}
		}
	})

	b.Run("compiled_binary", func(b *testing.B) {
		// Setup once BEFORE starting the timer
		if err := setupPlaybook(playbook); err != nil {
			b.Fatalf("Setup failed for %s: %v", playbook, err)
		}
		defer func() {
			_ = os.Remove("generated_tasks.go")
			_ = os.Remove("generated_tasks")
		}()

		// Reset timer to exclude setup time
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			cleanup()
			cmd := exec.Command("./generated_tasks", "--inventory", inventory, "--limit", "localhost", "--config", "test_artifacts/spage.yaml")
			if err := cmd.Run(); err != nil {
				b.Logf("Compiled binary run %d failed: %v", i, err)
			}
		}
	})
}
