package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Niclassslua/MoodleMCP/internal/app"
	mcpserver "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNormalizePage(t *testing.T) {
	tests := []struct {
		name                  string
		offset, limit         int
		wantOffset, wantLimit int
		wantErr               bool
	}{
		{name: "defaults", wantLimit: defaultPageSize},
		{name: "valid", offset: 8, limit: 5, wantOffset: 8, wantLimit: 5},
		{name: "negative offset", offset: -1, limit: 5, wantErr: true},
		{name: "too large", limit: maxPageSize + 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offset, limit, err := normalizePage(tt.offset, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (offset != tt.wantOffset || limit != tt.wantLimit) {
				t.Fatalf("got (%d, %d), want (%d, %d)", offset, limit, tt.wantOffset, tt.wantLimit)
			}
		})
	}
}

func TestPageBounds(t *testing.T) {
	start, end, more := pageBounds(25, 20, 20)
	if start != 20 || end != 25 || more {
		t.Fatalf("got (%d, %d, %v)", start, end, more)
	}
	start, end, more = pageBounds(25, 0, 20)
	if start != 0 || end != 20 || !more {
		t.Fatalf("got (%d, %d, %v)", start, end, more)
	}
	start, end, more = pageBounds(0, 0, 20)
	if start != 0 || end != 0 || more {
		t.Fatalf("empty bounds = (%d, %d, %v)", start, end, more)
	}
}

func TestValidateMode(t *testing.T) {
	for _, mode := range []string{"all", "resources-only", "forums-only"} {
		if err := validateMode(mode); err != nil {
			t.Errorf("valid mode %q: %v", mode, err)
		}
	}
	if err := validateMode("unknown"); err == nil {
		t.Error("expected unsupported mode to fail")
	}
}

func TestResolveOut(t *testing.T) {
	t.Setenv("MCD_OUT", "/data/moodle")
	for in, want := range map[string]string{"": "/data/moodle", "sub": "/data/moodle/sub", "/abs/x/": "/abs/x"} {
		if got, err := resolveOut(in); err != nil || got != want {
			t.Errorf("resolveOut(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	t.Setenv("MCD_OUT", "rel")
	if _, err := resolveOut(""); err == nil {
		t.Error("relative MCD_OUT should fail")
	}
}

// The server must start without Moodle and report login failures per call.
func TestConnectFailureIsToolError(t *testing.T) {
	calls := 0
	cs := connectClient(t, newServer("test", func() (*app.App, error) {
		calls++
		return nil, errors.New("connection refused")
	}))
	for _, name := range []string{"list_courses", "doctor"} {
		res, err := cs.CallTool(context.Background(), &mcpserver.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		text := res.Content[0].(*mcpserver.TextContent).Text
		if !res.IsError || !strings.Contains(text, "Moodle connection or login failed") {
			t.Errorf("%s: isError=%v text=%q", name, res.IsError, text)
		}
	}
	if calls != 2 {
		t.Errorf("connect attempts = %d, want 2 (failures must not be cached)", calls)
	}
}

func connectClient(t *testing.T, server *mcpserver.Server) *mcpserver.ClientSession {
	t.Helper()
	client := mcpserver.NewClient(&mcpserver.Implementation{Name: "test", Version: "1"}, nil)
	clientTransport, serverTransport := mcpserver.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serverSession.Close() })
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { clientSession.Close() })
	return clientSession
}

func TestToolsPublishSchemasAndBehaviorHints(t *testing.T) {
	server := newServer("test", func() (*app.App, error) { return nil, errors.New("offline") })
	client := mcpserver.NewClient(&mcpserver.Implementation{Name: "test", Version: "1"}, nil)
	clientTransport, serverTransport := mcpserver.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	byName := make(map[string]mcpserver.Tool)
	for tool, err := range clientSession.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		byName[tool.Name] = *tool
	}
	for _, name := range []string{"list_courses", "inspect_course", "get_conversation", "export_messages"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("missing tool %q", name)
		}
	}
	for name, tool := range byName {
		if tool.OutputSchema == nil {
			continue
		}
		b, _ := json.Marshal(tool.OutputSchema)
		var schema struct{ Type string }
		json.Unmarshal(b, &schema)
		if schema.Type != "object" {
			t.Errorf("%s: outputSchema type %q, MCP requires \"object\"", name, schema.Type)
		}
	}
	if byName["list_courses"].OutputSchema == nil {
		t.Error("list_courses has no output schema")
	}
	if byName["list_courses"].InputSchema == nil {
		t.Error("list_courses has no input schema")
	}
	if byName["get_conversation"].OutputSchema == nil {
		t.Error("get_conversation has no output schema")
	}
	if byName["list_courses"].Annotations == nil || !byName["list_courses"].Annotations.ReadOnlyHint {
		t.Error("list_courses should be marked read-only")
	}
	if byName["export_messages"].Annotations == nil || byName["export_messages"].Annotations.ReadOnlyHint {
		t.Error("export_messages should be marked as a write")
	}
}
