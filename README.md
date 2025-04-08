# Spage

This projects aims to function 'as' Ansible, but hugely more performant. By taking an Ansible playbook + inventory as input, it will generate a Go program that can be compiled for a specific host.
The end result is a generated .go file that can be compiled and shipped to the target host.

To create such a program, this project ships the `spage` binary, with which you can target Ansible playbooks + inventories. Output looks like this:

```bash
$ spage generate -p playbook.yaml
Processing node pkg.TaskNode "ensure were in arch iso" "shell": &{Execute:lsblk -f | grep "/run/archiso/bootmnt" && exit 0 || exit 1 Revert: ModuleInput:<nil>}
Processing node pkg.TaskNode "create ssh dir" "shell": &{Execute:mkdir -p .ssh Revert: ModuleInput:<nil>}
Processing node pkg.TaskNode "copy ssh key" "shell": &{Execute:curl -sSL https://github.com/AlexanderGrooff.keys > .ssh/authorized_keys Revert: ModuleInput:<nil>}
Compiling graph to code:
- Step 0:
  - ensure were in arch iso
  - create ssh dir
- Step 1:
  - copy ssh key
Required inputs:
Processing node pkg.TaskNode "ensure were in arch iso" "shell": &{Execute:lsblk -f | grep "/run/archiso/bootmnt" && exit 0 || exit 1 Revert: ModuleInput:<nil>}
Processing node pkg.TaskNode "create ssh dir" "shell": &{Execute:mkdir -p .ssh Revert: ModuleInput:<nil>}
Processing node pkg.TaskNode "copy ssh key" "shell": &{Execute:curl -sSL https://github.com/AlexanderGrooff.keys > .ssh/authorized_keys Revert: ModuleInput:<nil>}
Compiling graph to code:
- Step 0:
  - ensure were in arch iso
  - create ssh dir
- Step 1:
  - copy ssh key
Required inputs:
```

This will generate a `generated/tasks.go` file, which can be compiled for a specific host. That file looks like this:

```go
package generated

import (
    "github.com/AlexanderGrooff/spage/pkg"
    "github.com/AlexanderGrooff/spage/pkg/modules"
)

var GeneratedGraph = pkg.Graph{
  RequiredInputs: []string{
  },
  Tasks: [][]pkg.GraphNode{
      []pkg.GraphNode{
          pkg.Task{Name: "ensure were in arch iso", Module: "shell", Register: "", Params: modules.ShellInput{Execute: "lsblk -f | grep \"/run/archiso/bootmnt\" && exit 0 || exit 1", Revert: ""}, RunAs: "", When: ""},
          pkg.Task{Name: "create ssh dir", Module: "shell", Register: "", Params: modules.ShellInput{Execute: "mkdir -p .ssh", Revert: ""}, RunAs: "", When: ""},
      },
      []pkg.GraphNode{
          pkg.Task{Name: "copy ssh key", Module: "shell", Register: "", Params: modules.ShellInput{Execute: "curl -sSL https://github.com/AlexanderGrooff.keys > .ssh/authorized_keys", Revert: ""}, RunAs: "", When: ""},
      },
  },
}
```

## Project structure

`spage` makes use of modules to execute tasks, just like Ansible. Modules are located in the `pkg/modules` directory. This includes modules such as `shell`, `template`, `systemd`, etc.

## Usage

```bash
go generate
# OR
go run . generate -p playbook.yaml

# Run across an inventory
go run main.go -i inventory.yaml
# Or compile for a specific host and run
go run main.go -i inventory.yaml -H host1
```

TODO:

- Add more modules
- Implement task when
- Implement validation step
- Add revert conditions `revert_when`
- Make template variables case insensitive
- Allow variables to not start with a dot
- Should we compile assets (templates, files) along with the code?
- Hook up host facts like release, os, etc.

## Differences between Spage and Ansible

Inventory:

- `host` in inventory instead of `ansible_host`
- `groups` string under a host

Templating:

- Use `text/template` instead of Jinja
