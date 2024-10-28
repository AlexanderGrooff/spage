```bash
go generate
# OR
go run generate_tasks.go -file task.yaml

go run main.go -i inventory.yaml
```

TODO:

- Add more modules
- Validate module input
- Implement task variables
- Implement task dependencies
- Implement task when
- Implement validation step
- Generate graph
- Always run parallel
- Add revert conditions `revert_when`
- Make template variables case insensitive

## Differences between Spage and Ansible

Inventory:

- `host` in inventory instead of `ansible_host`
- `groups` string under a host

Templating:

- Use `text/template` instead of Jinja
