# Spage

This projects aims to function 'as' Ansible, but hugely more performant. By taking an Ansible playbook + inventory as
input, it will generate a Go program that can be compiled for a specific host.
The end result is a generated `.go` file that can be compiled and shipped to the target host.

To create such a program, this project ships the `spage` binary, with which you can target Ansible playbooks + inventories.

Key benefits:

- **(Almost full) Ansible compatibility** - Any playbook that works with Ansible works with Spage
- **Significantly faster execution** - Compiles to native Go code instead of interpreting Python
- **No Python dependency** - Single binary that can run anywhere
- **Extended features** - Built-in parallel execution, automatic rollback on failure, variable usage detection, and
support for external executors (like `temporal`)
- **Same syntax, but extra keywords** - Uses identical YAML playbook format and module parameters, with extra options
for parallel execution with `before`/`after`

Spage works by:

1. Taking your existing Ansible playbooks and inventory files
2. Generating Go code that implements the same logic
3. Compiling this into a single binary for your target environment
4. Executing tasks with native Go modules where possible, falling back to Python for full compatibility

## Installation

The fastest way to install Spage is using our installation script:

```bash
curl -sSL https://spage.dev/install.sh | bash
```

This will automatically detect your OS and architecture, download the latest release, and install it to `/usr/local/bin/spage`.

For other installation methods (manual download, Go install, building from source), see the [Installation Guide](https://spage.dev/docs/installation).

## Usage

You can use Spage in two ways:

1. Generate the Go code that you can then compile and run (using the `spage generate` command)
2. Run directly across an inventory (using the `spage run` command)

```bash
# Generate the Go code that you can then compile and run
spage generate --playbook playbook.yaml --output generated_tasks.go
go build -o spage_playbook generated_tasks.go
./spage_playbook --inventory inventory.yaml

# Or run directly across an inventory
spage run --inventory inventory.yaml --playbook playbook.yaml
```

## FAQ

### Q: What is Spage?

**A: Spage is a high-performance drop-in replacement for Ansible** that compiles your
playbooks into Go programs. You can then utilize the Golang toolchain to compile and run the playbook, either locally
or on the target host.

### Q: Why should I use Spage?

**A: Spage is a drop-in replacement for Ansible** that compiles your playbooks into Go programs. You can then utilize the
Golang toolchain to compile and run the playbook, either locally or on the target host.

If you're familiar with Ansible but want to improve performance, compile to a binary, get
rid of the Python dependency or want to adopt different execution models
such as running in Temporal, Spage is for you.
Instead of running `ansible-playbook playbook.yaml`, you can run
`spage run playbook.yaml` or `spage generate playbook.yaml -o generated_tasks.go && go run generated_tasks.go`.
Typical variables such as `--become`, `--inventory`, `--tags`, `--extra-vars` are supported.

### Q: Why is it called Spage?

**A: It's a reference to [Factorio: Space Age](https://www.factorio.com/space-age/buy).** I built Spage when Space Age was
not yet released, and I wanted something to do. So while waiting for the release, I
found a very funny Reddit comment calling it Spage, and thus Spage was born.

### Q: I have module `x.y.z` from an Ansible Galaxy collection. Is this supported in Spage?

**A: Yes, absolutely!** Spage supports **any** Ansible module, including those from Ansible Galaxy collections, through
its Python fallback mechanism. See the [Python Fallback Mechanism](#python-fallback-mechanism) section for more details.

## Python Fallback Mechanism

Spage includes a sophisticated Python fallback mechanism that allows it to execute any Ansible module, even those not
natively implemented in Go. This ensures 100% compatibility with the Ansible ecosystem while maintaining performance benefits.

### When Python Fallback is Used

The Python fallback automatically activates when:

- A module name is not found in Spage's native Go modules
- You explicitly use the `ansible_python` module type
- Community collections or custom modules are referenced (e.g., `community.general.setup`, `custom.namespace.module`)

### How It Works

1. **Module Detection**: Spage first attempts to find a native Go implementation of the requested module.
2. **Fallback Activation**: If no native module exists, the Python fallback mechanism engages.
3. **Playbook Generation**: A temporary Ansible playbook is generated, containing the required module and its parameters.
4. **Remote Execution**: Spage executes the playbook on the target host using `ansible-playbook`.

### Usage Examples

```yaml
# The 'command' module is supported natively by Spage, so it doesn't use the Python fallback
- name: Get Python requirements info
  command:
    cmd: echo "Hello world!"

# Community collection module is not (yet) supported by Spage, so it uses the Python fallback
- name: Get Python requirements info
  community.general.python_requirements_info:
    dependencies: []

# Custom namespace module is also using the Python fallback
- name: Use custom module
  my_company.custom_collection.special_module:
    param1: value1
    param2: value2

# Explicit Python fallback
- name: Force usage of Python fallback for ping module
  ansible_python:
    module_name: ping
    args:
      data: pong
```

### Limitations

- Requires Python 3 and pip on the executing host for collection installation
- Some complex Ansible plugins may have additional dependencies

## Differences between Spage and Ansible

Spage is a drop-in replacement for Ansible, but with some notable differences:

- Playbooks are allowed to start without `- tasks:`. It assumes `hosts: localhost` and runs locally.
- Tasks are executed in parallel by default based on variable usage.
- New keywords `before`/`after` are available to control the flow of parallel tasks.
- The `shell` module has two new parameters: `execute` and `revert`. If you don't specify these options and just use it
as you would with Ansible, it will not do anything on revert.

TODO:

- Add revert conditions `revert_when`
- Should we compile assets (templates, files) along with the code?
- `vars_prompt` on play
- `gather_facts` on play
- Logic for `no_log`
- Plugin support
- Callback support
