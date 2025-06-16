package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
logging:
  level: "debug"
  file: "test.log"

security:
  enable_tls: true
  cert_file: "cert.pem"
  key_file: "key.pem"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	tests := []struct {
		name        string
		configPaths []string
		envVars     map[string]string
		want        *Config
		wantErr     bool
	}{
		{
			name:        "default config",
			configPaths: []string{},
			want: &Config{
				Logging: LoggingConfig{
					Level: "info",
					File:  "",
				},
			},
		},
		{
			name:        "config from file",
			configPaths: []string{configPath},
			want: &Config{
				Logging: LoggingConfig{
					Level: "debug",
					File:  "test.log",
				},
			},
		},
		{
			name:        "config from env vars",
			configPaths: []string{},
			envVars: map[string]string{
				"SPAGE_LOGGING_LEVEL":       "warn",
				"SPAGE_SECURITY_ENABLE_TLS": "true",
			},
			want: &Config{
				Logging: LoggingConfig{
					Level: "warn",
					File:  "",
				},
			},
		},
		{
			name:        "invalid config file",
			configPaths: []string{"nonexistent.yaml"},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			got, err := Load(tt.configPaths...)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
