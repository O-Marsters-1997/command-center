package config

import (
	"encoding/json"
	"fmt"
)

const MCPFile = ".mcp.json"

type mcpServers struct {
	Servers map[string]json.RawMessage `json:"mcpServers"`
}

func ResolveMCP(names []string, repoBody, userBody []byte) ([]byte, error) {
	if len(names) == 0 {
		return nil, nil
	}
	var repo, user mcpServers
	for _, src := range []struct {
		label string
		body  []byte
		into  *mcpServers
	}{{MCPFile, repoBody, &repo}, {"~/.claude.json", userBody, &user}} {
		if len(src.body) == 0 {
			continue
		}
		if err := json.Unmarshal(src.body, src.into); err != nil {
			return nil, fmt.Errorf("decode %s: %w", src.label, err)
		}
	}
	picked := make(map[string]json.RawMessage, len(names))
	for _, name := range names {
		def, ok := repo.Servers[name]
		if !ok {
			def, ok = user.Servers[name]
		}
		if !ok {
			return nil, fmt.Errorf("mcp server %q is in neither %s nor ~/.claude.json", name, MCPFile)
		}
		picked[name] = def
	}
	out, err := json.Marshal(mcpServers{Servers: picked})
	if err != nil {
		return nil, fmt.Errorf("encode mcp config: %w", err)
	}
	return out, nil
}
