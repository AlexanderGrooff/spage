```bash
go generate
# OR
go run generate_tasks.go -file task.yaml

go run main.go -i inventory.yaml
```

TODO:

- Add more modules
- Implement task when
- Implement validation step
- Add revert conditions `revert_when`
- Make template variables case insensitive
- Should we compile assets (templates, files) along with the code?
- Hook up host facts like release, os, etc.

## Differences between Spage and Ansible

Inventory:

- `host` in inventory instead of `ansible_host`
- `groups` string under a host

Templating:

- Use `text/template` instead of Jinja
