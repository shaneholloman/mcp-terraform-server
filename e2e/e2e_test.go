// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package e2e

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	mcpClient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

var initOnce sync.Once
var globalClient mcpClient.MCPClient
var globalCleanup func()

func TestE2E(t *testing.T) {
	buildDockerImage(t)
	
	// Ensure all test containers are cleaned up at the end
	t.Cleanup(func() {
		cleanupAllTestContainers(t)
	})
	
	testCases := []struct {
		name string
		clientFactory func(t *testing.T) (mcpClient.MCPClient, func())
	}{
		{"Stdio", createStdioClient},
		{"HTTP", createHTTPClient},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, cleanup := tc.clientFactory(t)
			defer cleanup()
			runTestSuite(t, client, tc.name)
		})
	}
}

// ensureClientInitialized ensures the MCP client is initialized before running tool tests
func ensureClientInitialized(t *testing.T, client mcpClient.MCPClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{
		Name:    "e2e-test-client",
		Version: "0.0.1",
	}

	result, err := client.Initialize(ctx, request)
	if err != nil {
		t.Fatalf("Failed to initialize MCP client: %v", err)
	}
	t.Logf("Initialized with server: %s %s", result.ServerInfo.Name, result.ServerInfo.Version)
	require.Equal(t, "terraform-mcp-server", result.ServerInfo.Name)
}

// runTestSuite executes all test cases against the provided client
func runTestSuite(t *testing.T, client mcpClient.MCPClient, transportName string) {
	t.Run("Initialize", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		request := mcp.InitializeRequest{}
		request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		request.Params.ClientInfo = mcp.Implementation{
			Name:    "e2e-test-client",
			Version: "0.0.1",
		}

		result, err := client.Initialize(ctx, request)
		if err != nil {
			log.Fatalf("Failed to initialize: %v", err)
		}
		fmt.Printf(
			"Initialized with server: %s %s\n\n",
			result.ServerInfo.Name,
			result.ServerInfo.Version,
		)
		require.Equal(t, "terraform-mcp-server", result.ServerInfo.Name)
	})

	for _, testCase := range providerTestCases {
		t.Run(fmt.Sprintf("%s_resolveProviderDocID/%s", transportName, testCase.TestName), func(t *testing.T) {
			ensureClientInitialized(t, client)
			t.Logf("TOOL resolveProviderDocID %s", testCase.TestDescription)
			t.Logf("Test payload: %v", testCase.TestPayload)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			request := mcp.CallToolRequest{}
			request.Params.Name = "resolveProviderDocID"
			request.Params.Arguments = testCase.TestPayload

			response, err := client.CallTool(ctx, request)
			if testCase.TestShouldFail {
				require.Error(t, err, "expected to call 'resolveProviderDocID' tool with error")
				t.Logf("Error: %v", err)
			} else {
				require.NoError(t, err, "expected to call 'resolveProviderDocID' tool successfully")
				require.False(t, response.IsError, "expected result not to be an error")
				require.Len(t, response.Content, 1, "expected content to have one item")

				textContent, ok := response.Content[0].(mcp.TextContent)
				require.True(t, ok, "expected content to be of type TextContent")
				t.Logf("Content length: %d", len(textContent.Text))

				if testCase.TestContentType == CONST_TYPE_DATA_SOURCE {
					require.Contains(t, textContent.Text, "Category: data-sources", "expected content to contain data-sources")
				} else if testCase.TestContentType == CONST_TYPE_RESOURCE {
					require.Contains(t, textContent.Text, "Category: resources", "expected content to contain resources")
				} else if testCase.TestContentType == CONST_TYPE_GUIDES {
					require.Contains(t, textContent.Text, "guide", "expected content to contain guide")
				} else if testCase.TestContentType == CONST_TYPE_FUNCTIONS {
					require.Contains(t, textContent.Text, "functions", "expected content to contain functions")
				}
			}
		})
	}

	for _, testCase := range providerDocsTestCases {
		t.Run(fmt.Sprintf("%s_getProviderDocs/%s", transportName, testCase.TestName), func(t *testing.T) {
			ensureClientInitialized(t, client)
			t.Logf("TOOL getProviderDocs %s", testCase.TestDescription)
			t.Logf("Test payload: %v", testCase.TestPayload)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			request := mcp.CallToolRequest{}
			request.Params.Name = "getProviderDocs"
			request.Params.Arguments = testCase.TestPayload

			response, err := client.CallTool(ctx, request)
			if testCase.TestShouldFail {
				require.Error(t, err, "expected to call 'getProviderDocs' tool with error")
				t.Logf("Error: %v", err)
			} else {
				require.NoError(t, err, "expected to call 'getProviderDocs' tool successfully")
				require.False(t, response.IsError, "expected result not to be an error")
				require.Len(t, response.Content, 1, "expected content to have one item")

				textContent, ok := response.Content[0].(mcp.TextContent)
				require.True(t, ok, "expected content to be of type TextContent")
				t.Logf("Content length: %d", len(textContent.Text))

				require.Contains(t, textContent.Text, "page_title", "expected content to contain a page_title")
			}
		})
	}

	for _, testCase := range searchModulesTestCases {
		t.Run(fmt.Sprintf("%s_searchModules/%s", transportName, testCase.TestName), func(t *testing.T) {
			ensureClientInitialized(t, client)
			t.Logf("TOOL searchModules %s", testCase.TestDescription)
			t.Logf("Test payload: %v", testCase.TestPayload)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			request := mcp.CallToolRequest{}
			request.Params.Name = "searchModules"
			request.Params.Arguments = testCase.TestPayload

			response, err := client.CallTool(ctx, request)
			if testCase.TestShouldFail {
				require.Error(t, err, "expected to call 'searchModules' tool with error")
				t.Logf("Error: %v", err)
			} else {
				require.NoError(t, err, "expected to call 'searchModules' tool successfully")
				require.False(t, response.IsError, "expected result not to be an error")
				if len(response.Content) > 0 {
					textContent, ok := response.Content[0].(mcp.TextContent)
					require.True(t, ok, "expected content to be of type TextContent")
					t.Logf("Content length: %d", len(textContent.Text))
				} else {
					t.Log("Response content is empty for successful call.")
				}
			}
		})
	}

	for _, testCase := range moduleDetailsTestCases {
		t.Run(fmt.Sprintf("%s_moduleDetails/%s", transportName, testCase.TestName), func(t *testing.T) {
			ensureClientInitialized(t, client)
			t.Logf("TOOL moduleDetails %s", testCase.TestDescription)
			t.Logf("Test payload: %v", testCase.TestPayload)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			request := mcp.CallToolRequest{}
			request.Params.Name = "moduleDetails"
			request.Params.Arguments = testCase.TestPayload

			response, err := client.CallTool(ctx, request)
			if testCase.TestShouldFail {
				require.Error(t, err, "expected to call 'moduleDetails' tool with error")
				t.Logf("Error: %v", err)
			} else {
				require.NoError(t, err, "expected to call 'moduleDetails' tool successfully")
				require.False(t, response.IsError, "expected result not to be an error")
				require.Len(t, response.Content, 1, "expected content to have one item")

				textContent, ok := response.Content[0].(mcp.TextContent)
				require.True(t, ok, "expected content to be of type TextContent")
				t.Logf("Content length: %d", len(textContent.Text))

				if testCase.TestContentType == CONST_TYPE_DATA_SOURCE {
					require.NotContains(t, textContent.Text, "**Category:** resources", "expected content not to contain resources")
				} else if testCase.TestContentType == CONST_TYPE_RESOURCE {
					require.NotContains(t, textContent.Text, "**Category:** data-sources", "expected content not to contain data-sources")
				}
			}
		})
	}
}

// createStdioClient creates a stdio-based MCP client
func createStdioClient(t *testing.T) (mcpClient.MCPClient, func()) {
	args := []string{
		"docker",
		"run",
		"-i",
		"--rm",
		"terraform-mcp-server:test-e2e",
	}
	t.Log("Starting Stdio MCP client...")
	client, err := mcpClient.NewStdioMCPClient(args[0], []string{}, args[1:]...)
	require.NoError(t, err, "expected to create stdio client successfully")
	
	cleanup := func() {
		client.Close()
	}
	
	return client, cleanup
}

// createHTTPClient creates an HTTP-based MCP client
func createHTTPClient(t *testing.T) (mcpClient.MCPClient, func()) {
	t.Log("Starting HTTP MCP server...")
	
	port := getTestPort()
	baseURL := fmt.Sprintf("http://localhost:%s", port)
	mcpURL := fmt.Sprintf("http://localhost:%s/mcp", port)

	// Start container in HTTP mode
	containerID := startHTTPContainer(t, port)
	
	// Ensure container cleanup even if test fails
	t.Cleanup(func() {
		stopContainer(t, containerID)
	})
	
	// Wait for server to be ready
	waitForServer(t, baseURL)
	
	// Create client with MCP endpoint
	client, err := mcpClient.NewStreamableHttpClient(mcpURL)
	require.NoError(t, err, "expected to create HTTP client successfully")
	
	cleanup := func() {
		if client != nil {
			client.Close()
		}
		// Container cleanup handled by t.Cleanup()
	}
	
	return client, cleanup
}

// startHTTPContainer starts a Docker container in HTTP mode and returns container ID
func startHTTPContainer(t *testing.T, port string) string {
	portMapping := fmt.Sprintf("%s:8080", port)
	cmd := exec.Command("docker", "run", "-d", "--rm", "-e", "MODE=http", "-p", portMapping, "terraform-mcp-server:test-e2e")
	output, err := cmd.Output()
	require.NoError(t, err, "expected to start HTTP container successfully")
	
	containerID := string(output)[:12] // First 12 chars of container ID
	t.Logf("Started HTTP container: %s on port %s", containerID, port)
	return containerID
}

// waitForServer waits for the HTTP server to be ready
func waitForServer(t *testing.T, baseURL string) {
	client := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 30; i++ {
		resp, err := client.Get(baseURL + "/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			t.Log("HTTP server is ready")
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatal("HTTP server failed to start within 30 seconds")
}

// stopContainer stops the Docker container
func stopContainer(t *testing.T, containerID string) {
	if containerID == "" {
		return
	}
	
	t.Logf("Stopping container: %s", containerID)
	cmd := exec.Command("docker", "stop", containerID)
	if err := cmd.Run(); err != nil {
		t.Logf("Warning: failed to stop container %s: %v", containerID, err)
		// Try force kill if stop fails
		killCmd := exec.Command("docker", "kill", containerID)
		if killErr := killCmd.Run(); killErr != nil {
			t.Logf("Warning: failed to kill container %s: %v", containerID, killErr)
		}
	} else {
		t.Logf("Successfully stopped container: %s", containerID)
	}
}

// cleanupAllTestContainers stops all containers created by this test
func cleanupAllTestContainers(t *testing.T) {
	t.Log("Cleaning up all test containers...")
	
	// Find all containers with our test image
	cmd := exec.Command("docker", "ps", "-q", "--filter", "ancestor=terraform-mcp-server:test-e2e")
	output, err := cmd.Output()
	if err != nil {
		t.Logf("Warning: failed to list test containers: %v", err)
		return
	}
	
	containerIDs := string(output)
	if containerIDs == "" {
		t.Log("No test containers found to cleanup")
		return
	}
	
	// Stop all found containers
	stopCmd := exec.Command("docker", "stop")
	stopCmd.Stdin = strings.NewReader(containerIDs)
	if err := stopCmd.Run(); err != nil {
		t.Logf("Warning: failed to stop some test containers: %v", err)
	} else {
		t.Log("Successfully cleaned up all test containers")
	}
}

// getTestPort returns the test port from environment variable or default
func getTestPort() string {
	if port := os.Getenv("E2E_TEST_PORT"); port != "" {
		return port
	}
	return "8080"
}

func buildDockerImage(t *testing.T) {
	t.Log("Building Docker image for e2e tests...")

	cmd := exec.Command("make", "VERSION=test-e2e", "docker-build")
	cmd.Dir = ".." // Run this in the context of the root, where the Makefile is located.
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "expected to build Docker image successfully, output: %s", string(output))
}
