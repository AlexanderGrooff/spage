#!/usr/bin/env python3
import json
print(json.dumps({
  "_meta": {"hostvars": {}},
  "all": {"hosts": ["web01.example.com", "web02.example.com", "db01.example.com"]}
}))
