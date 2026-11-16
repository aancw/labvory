package api

import (
	"net/http"

	"github.com/petruknisme/lanvory/internal/httpx"
	"github.com/petruknisme/lanvory/internal/store"
)

// TopologyGraph is the React Flow data source: every asset as a node, every
// relationship and parent-child link as an edge.
type TopologyGraph struct {
	Nodes []TopologyNode `json:"nodes"`
	Edges []TopologyEdge `json:"edges"`
}

type TopologyNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Category string `json:"category"`
	TypeSlug string `json:"type_slug"`
	TypeLabel string `json:"type_label"`
	Status   string `json:"status"`
	ParentID string `json:"parent_id,omitempty"`
	PrimaryIP string `json:"primary_ip,omitempty"`
}

type TopologyEdge struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	Kind       string `json:"kind"` // hosts | runs_on | connected_to | depends_on
	Note       string `json:"note,omitempty"`
	SourcePort string `json:"source_port,omitempty"`
	TargetPort string `json:"target_port,omitempty"`
}

// handleTopology assembles the full inventory graph. Physical links are not
// invented: only explicit parent-child and relationship edges are returned.
func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	assets, _, err := s.assets.List(ctx, store.AssetFilter{Limit: 1000, Sort: "name", Order: "asc"})
	if err != nil {
		s.logger.Error("topology: assets failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load topology.", nil)
		return
	}

	nodes := make([]TopologyNode, 0, len(assets))
	assetIDs := map[string]bool{}
	for _, a := range assets {
		assetIDs[a.ID] = true
		node := TopologyNode{
			ID: a.ID, Name: a.Name, Hostname: a.Hostname,
			Category: a.Category, TypeSlug: a.TypeSlug, TypeLabel: a.TypeLabel,
			Status: a.Status, ParentID: derefStr(a.ParentID), PrimaryIP: derefStr(a.PrimaryIP),
		}
		nodes = append(nodes, node)
	}

	edges := []TopologyEdge{}

	// Parent-child edges (virtualization view).
	for _, a := range assets {
		if a.ParentID != nil && assetIDs[*a.ParentID] {
			edges = append(edges, TopologyEdge{
				ID: "hosts-" + a.ID, Source: *a.ParentID, Target: a.ID, Kind: "hosts",
			})
		}
	}

	// Explicit relationships.
	rels, err := s.relationships.All(ctx)
	if err != nil {
		s.logger.Error("topology: relationships failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load topology.", nil)
		return
	}
	for _, rel := range rels {
		if !assetIDs[rel.SourceID] || !assetIDs[rel.TargetID] {
			continue // dangling edge to a deleted asset
		}
		edges = append(edges, TopologyEdge{
			ID: rel.ID, Source: rel.SourceID, Target: rel.TargetID,
			Kind: rel.Kind, Note: rel.Note,
			SourcePort: rel.SourcePort, TargetPort: rel.TargetPort,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, TopologyGraph{Nodes: nodes, Edges: edges})
}
