//go:generate go run generate_tasks.go -file playbook.yaml

package main

func main() {
	RootCmd.Execute()
}
