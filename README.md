```bash
go generate
# OR
go run generate_tasks.go -file task.yaml

go run main.go -i inventory.yaml
```

TODO:

- Rename project (reconcile doesn't make sense)
- Add more modules
- Add inventory support
- Validate module input
- Do this instead of module: and params:
  - shell:
      param1:
      param2:
- Implement task variables
- Implement task dependencies
- Implement task when
- Implement validation step
- Generate graph
- Always run parallel

## Differences between Spage and Ansible

Inventory:

- `host` in inventory instead of `ansible_host`
- `groups` string under 
