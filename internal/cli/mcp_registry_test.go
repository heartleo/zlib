package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newMCPFlagCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "mcp"}
	addMCPFlags(cmd)
	return cmd
}

// Every flag can be set from ZLIB_MCP_<FLAG>, a flag on the command line wins,
// an empty variable is unset, and a bad value is an error naming the variable.
func TestMCPFlagsReadTheEnvironment(t *testing.T) {
	if mcpEnvName("read-only") != "ZLIB_MCP_READ_ONLY" || mcpEnvName("allowed-dir") != "ZLIB_MCP_ALLOWED_DIR" {
		t.Fatalf("names: %q %q", mcpEnvName("read-only"), mcpEnvName("allowed-dir"))
	}
	t.Setenv("ZLIB_MCP_READ_ONLY", "true")
	t.Setenv("ZLIB_MCP_ALLOWED_DIR", "  /books  ")
	t.Setenv("ZLIB_MCP_DISABLE_KINDLE", "") // a client's blank setting
	t.Setenv("ZLIB_MCP_LOG_LEVEL", "debug")
	cmd := newMCPFlagCommand()
	if err := applyMCPEnv(cmd); err != nil {
		t.Fatal(err)
	}
	ro, _ := cmd.Flags().GetBool("read-only")
	dir, _ := cmd.Flags().GetString("allowed-dir")
	kindle, _ := cmd.Flags().GetBool("disable-kindle")
	level, _ := cmd.Flags().GetString("log-level")
	if !ro || dir != "/books" || kindle || level != "debug" {
		t.Errorf("read-only %v, allowed-dir %q, disable-kindle %v, log-level %q", ro, dir, kindle, level)
	}

	// The command line wins.
	cmd = newMCPFlagCommand()
	if err := cmd.Flags().Parse([]string{"--read-only=false", "--allowed-dir", "/other"}); err != nil {
		t.Fatal(err)
	}
	if err := applyMCPEnv(cmd); err != nil {
		t.Fatal(err)
	}
	if ro, _ := cmd.Flags().GetBool("read-only"); ro {
		t.Error("the environment overrode --read-only=false")
	}
	if dir, _ := cmd.Flags().GetString("allowed-dir"); dir != "/other" {
		t.Errorf("allowed-dir = %q", dir)
	}

	t.Setenv("ZLIB_MCP_READ_ONLY", "sometimes")
	if err := applyMCPEnv(newMCPFlagCommand()); err == nil || !strings.Contains(err.Error(), "ZLIB_MCP_READ_ONLY") {
		t.Errorf("a bad value: %v", err)
	}
}

func readJSONFile(t *testing.T, rel string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
}

// The registry entry and the bundle's manifest describe the server that runs:
// the same description, the tools it registers, and the settings it reads.
func TestRegistryFilesMatchTheServer(t *testing.T) {
	var server struct {
		Name, Description string
		WebsiteURL        string `json:"websiteUrl"`
		Packages          []struct {
			EnvironmentVariables []struct{ Name string } `json:"environmentVariables"`
		}
	}
	readJSONFile(t, "packaging/mcp/server.json", &server)
	var manifest struct {
		Description string
		Tools       []struct{ Name string }
		Server      struct {
			MCPConfig struct{ Env map[string]string } `json:"mcp_config"`
		}
		UserConfig map[string]json.RawMessage `json:"user_config"`
	}
	readJSONFile(t, "packaging/mcpb/manifest.json", &manifest)

	if server.Description != mcpServerDescription || manifest.Description != mcpServerDescription || len([]rune(mcpServerDescription)) > 100 {
		t.Errorf("descriptions differ or exceed 100 characters:\n%q\n%q\n%q", server.Description, manifest.Description, mcpServerDescription)
	}
	if server.WebsiteURL != mcpWebsiteURL {
		t.Errorf("websiteUrl = %q, want %q", server.WebsiteURL, mcpWebsiteURL)
	}

	session := connectMCPTestSession(t, mcpServerOptions{}, nil)
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var registered, listed []string
	for _, tool := range tools.Tools {
		registered = append(registered, tool.Name)
	}
	for _, tool := range manifest.Tools {
		listed = append(listed, tool.Name)
	}
	slices.Sort(registered)
	slices.Sort(listed)
	if !slices.Equal(registered, listed) {
		t.Errorf("the manifest lists %v, the server registers %v", listed, registered)
	}
	init := session.InitializeResult()
	if init.ServerInfo.Description != mcpServerDescription || init.ServerInfo.WebsiteURL != mcpWebsiteURL || len(init.ServerInfo.Icons) == 0 {
		t.Errorf("server info = %+v", init.ServerInfo)
	}

	// Each setting the files pass on is one the command reads, and the bundle's
	// settings are the ones it maps into them.
	want := []string{mcpEnvName("read-only"), mcpEnvName("allowed-dir")}
	var documented, mapped []string
	for _, v := range server.Packages[0].EnvironmentVariables {
		documented = append(documented, v.Name)
	}
	for name, value := range manifest.Server.MCPConfig.Env {
		mapped = append(mapped, name)
		key := strings.TrimSuffix(strings.TrimPrefix(value, "${user_config."), "}")
		if _, ok := manifest.UserConfig[key]; !ok {
			t.Errorf("%s reads %s, which is not a user setting", name, value)
		}
	}
	slices.Sort(documented)
	slices.Sort(mapped)
	slices.Sort(want)
	if !slices.Equal(documented, want) || !slices.Equal(mapped, want) {
		t.Errorf("documented %v, mapped %v, want %v", documented, mapped, want)
	}
	cmd := newMCPFlagCommand()
	for _, name := range want {
		found := false
		cmd.Flags().VisitAll(func(f *pflag.Flag) { found = found || mcpEnvName(f.Name) == name })
		if !found {
			t.Errorf("no flag reads %s", name)
		}
	}
}
