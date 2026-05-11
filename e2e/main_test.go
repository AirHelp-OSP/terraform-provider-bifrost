package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	bifrostImage    = "maximhq/bifrost:v1.5.0"
	bifrostPort     = "8080/tcp"
	bifrostUsername = "admin"
	bifrostPassword = "testpassword123"
)

// Shared state across all tests in the package: the Bifrost container's
// exposed URL and the path to a tofurc that routes our provider via
// dev_overrides to a locally built binary.
var (
	bifrostEndpoint string
	tofuRCPath      string
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()

	providerDir, err := buildProvider(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build provider: %v\n", err)
		return 1
	}
	defer os.RemoveAll(providerDir)

	tofuRCPath, err = writeTofuRC(providerDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write tofurc: %v\n", err)
		return 1
	}
	defer os.Remove(tofuRCPath)

	container, err := startBifrost(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start bifrost: %v\n", err)
		return 1
	}
	defer func() { _ = container.Terminate(ctx) }()

	bifrostEndpoint, err = containerEndpoint(ctx, container)
	if err != nil {
		fmt.Fprintf(os.Stderr, "endpoint: %v\n", err)
		return 1
	}

	return m.Run()
}

// buildProvider compiles the provider and drops the binary in a fresh temp dir
// named so that dev_overrides can point at the directory.
func buildProvider(ctx context.Context) (string, error) {
	dir, err := os.MkdirTemp("", "tpb-bin-*")
	if err != nil {
		return "", err
	}
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "terraform-provider-bifrost")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("go build: %w", err)
	}
	return dir, nil
}

func writeTofuRC(providerDir string) (string, error) {
	f, err := os.CreateTemp("", "tofurc-*")
	if err != nil {
		return "", err
	}
	content := fmt.Sprintf(`provider_installation {
  dev_overrides {
    "registry.terraform.io/airhelp-osp/bifrost" = %q
  }
  direct {}
}
`, providerDir)
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return "", err
	}
	return f.Name(), f.Close()
}

func startBifrost(ctx context.Context) (testcontainers.Container, error) {
	req := testcontainers.ContainerRequest{
		Image:        bifrostImage,
		ExposedPorts: []string{bifrostPort},
		Env: map[string]string{
			"APP_PORT":               "8080",
			"APP_HOST":               "0.0.0.0",
			"LOG_LEVEL":              "info",
			"BIFROST_ADMIN_USERNAME": bifrostUsername,
			"BIFROST_ADMIN_PASSWORD": bifrostPassword,
		},
		WaitingFor: wait.ForHTTP("/health").
			WithPort(bifrostPort).
			WithStartupTimeout(90 * time.Second),
	}
	return testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
}

func containerEndpoint(ctx context.Context, c testcontainers.Container) (string, error) {
	host, err := c.Host(ctx)
	if err != nil {
		return "", err
	}
	port, err := c.MappedPort(ctx, bifrostPort)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://%s:%s", host, port.Port()), nil
}
