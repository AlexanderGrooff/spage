```bash
go generate
# OR
go run . generate -p playbook.yaml

# Run across an inventory
go run main.go -i inventory.yaml
# Or compile for a specific host and run
go run main.go -i inventory.yaml -H host1

# Run the web server
swag init -g pkg/web/server.go
go run main.go web
# Go to http://localhost:8080/docs/index.html

# Development with hot reload
go install github.com/cosmtrek/air@latest
air
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
