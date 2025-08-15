# Daemon Support

This project includes optional daemon support that provides enhanced monitoring and reporting capabilities. The daemon functionality requires the `spage-protobuf` dependency.

## Build Tags

The daemon functionality is implemented using Go build tags for conditional compilation:

- **Default build** (no daemon): The project builds with stub implementations that provide no-op daemon functionality
- **Daemon build** (`-tags daemon`): The project builds with full daemon support, requiring the protobuf dependency

## Building Without Daemon Support (Default)

```bash
# Standard build - no daemon functionality
go build ./...

# Run the application
./spage run playbook.yml
```

When building without the daemon tag:
- All daemon-related code is excluded from compilation
- Stub implementations provide no-op functionality
- No protobuf dependencies are required
- The application runs normally but without daemon reporting

## Building With Daemon Support

**Prerequisites:**
- Ensure `../spage-protobuf` is available and contains the required protobuf definitions
- The protobuf dependency should be properly set up as specified in `go.mod`

```bash
# Build with daemon support
go build -tags daemon ./...

# Run with daemon configuration
./spage run playbook.yml --daemon-grpc localhost:9091
```

## Configuration

When daemon support is enabled, you can configure it through:

### Command Line Flags:
- `--daemon-grpc`: gRPC endpoint for daemon communication (default: localhost:9091)
- `--play-id`: Unique identifier for the playbook execution

### Configuration File:
```yaml
daemon:
  enabled: true
  endpoint: "localhost:9091"
  play_id: "custom-play-id"
  timeout: 30s
```

## Functionality

With daemon support enabled, the application provides:
- Real-time task execution reporting
- Play start/completion notifications
- Task progress streaming
- Error reporting and metrics collection

## Development

When developing daemon-related features:

1. **Add build tags** to files that require protobuf dependencies:
   ```go
   //go:build daemon
   // +build daemon
   ```

2. **Create stub implementations** for non-daemon builds in separate files:
   ```go
   //go:build !daemon
   // +build !daemon
   ```

3. **Test both build configurations**:
   ```bash
   # Test without daemon
   go test ./...
   
   # Test with daemon (requires protobuf)
   go test -tags daemon ./...
   ```

## Troubleshooting

### "No packages found" warnings in IDE
If you see warnings about build tags in your IDE, configure gopls with build flags:
```json
{
  "go.buildFlags": ["-tags=daemon"]
}
```

### Missing protobuf dependency
If you get protobuf-related errors, ensure:
1. The `../spage-protobuf` directory exists
2. The protobuf definitions are up to date
3. Run `go mod tidy` to resolve dependencies
