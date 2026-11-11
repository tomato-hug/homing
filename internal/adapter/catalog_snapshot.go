package adapter

import (
	"embed"
	"encoding/json"
	"fmt"
)

// The snapshot keeps discovery metadata from Magpie's built-in agent table and
// its community Agent plugin registry. It deliberately contains no Magpie code
// and is read at build time: Homing does not call Magpie or npm at runtime.
// Source revisions live in catalogdata/SOURCES.md.
//
//go:embed catalogdata/magpie-agents.json
var catalogJSONFS embed.FS

type catalogSnapshot struct {
	Source        string        `json:"source"`
	SourceURL     string        `json:"source_url"`
	SnapshotDate  string        `json:"snapshot_date"`
	SourceVersion string        `json:"source_version"`
	Agents        []catalogSpec `json:"agents"`
}

func init() {
	catalogSpecs = append(catalogSpecs, mustLoadCatalogSnapshot().Agents...)
}

func mustLoadCatalogSnapshot() catalogSnapshot {
	raw, err := catalogJSONFS.ReadFile("catalogdata/magpie-agents.json")
	if err != nil {
		panic(fmt.Sprintf("catalog snapshot: %v", err))
	}
	var snapshot catalogSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		panic(fmt.Sprintf("catalog snapshot: %v", err))
	}
	for i := range snapshot.Agents {
		agent := &snapshot.Agents[i]
		if agent.CatalogSource == "" {
			agent.CatalogSource = "magpie"
		}
		if agent.SourceURL == "" {
			agent.SourceURL = snapshot.SourceURL
		}
	}
	return snapshot
}
