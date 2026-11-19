package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aancw/labvory/internal/integrations"
	"github.com/aancw/labvory/internal/store"
)

// TestKubernetesSync runs the connector against a fake Kubernetes API:
// nodes and pods become assets (pods parented to their node) and Services are
// imported into the service registry with endpoints.
func TestKubernetesSync(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	srv, _, _, do := newTestServer(t, databaseURL)
	bootAdmin(t, do)

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k8s-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"kind":"Status","status":"Failure","code":401}`))
			return
		}
		switch r.URL.Path {
		case "/api/v1/nodes":
			_, _ = w.Write([]byte(`{"items":[
				{"metadata":{"name":"node-1","uid":"n1","labels":{"node-role.kubernetes.io/control-plane":""}},
				 "status":{"conditions":[{"type":"Ready","status":"True"}],
				 "addresses":[{"type":"InternalIP","address":"10.0.0.11"}],
				 "capacity":{"cpu":"4","memory":"16Gi"},
				 "nodeInfo":{"kubeletVersion":"v1.31.2","containerRuntimeVersion":"containerd://2.0.0","osImage":"Ubuntu 24.04"}}},
				{"metadata":{"name":"node-2","uid":"n2","labels":{}},
				 "status":{"conditions":[{"type":"Ready","status":"False"}],
				 "capacity":{"cpu":"4500m","memory":"2048Mi"},
				 "nodeInfo":{"kubeletVersion":"v1.31.2","containerRuntimeVersion":"containerd://2.0.0","osImage":"Ubuntu 24.04"}}}
			]}`))
		case "/api/v1/pods":
			_, _ = w.Write([]byte(`{"items":[
				{"metadata":{"name":"nginx-1","namespace":"default","uid":"p1"},
				 "spec":{"nodeName":"node-1","containers":[{"name":"nginx","image":"nginx:1.27"}]},
				 "status":{"phase":"Running"}},
				{"metadata":{"name":"dns-1","namespace":"kube-system","uid":"p2"},
				 "spec":{"nodeName":"node-2","containers":[{"name":"coredns","image":"coredns:1.11"}]},
				 "status":{"phase":"Pending"}}
			]}`))
		case "/api/v1/services":
			_, _ = w.Write([]byte(`{"items":[
				{"metadata":{"name":"nginx","namespace":"default","uid":"s1"},
				 "spec":{"type":"ClusterIP","clusterIP":"10.96.0.10","ports":[{"name":"http","protocol":"TCP","port":80}]}},
				{"metadata":{"name":"kube-dns","namespace":"kube-system","uid":"s2"},
				 "spec":{"type":"ClusterIP","clusterIP":"10.96.0.10","ports":[{"name":"dns","protocol":"UDP","port":53}]}},
				{"metadata":{"name":"web","namespace":"default","uid":"s3"},
				 "spec":{"type":"NodePort","clusterIP":"10.96.0.11","ports":[{"name":"http","protocol":"TCP","port":80,"nodePort":30080}]}}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	integ := store.Integration{
		Kind:   "kubernetes",
		Name:   "k8s",
		Config: []byte(`{"api_server":"` + fake.URL + `","verify_tls":false}`),
	}
	summary, err := integrations.SyncKubernetes(context.Background(), srv.pool, integ, "k8s-token")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if summary["nodes"] != 2 || summary["pods"] != 2 || summary["services"] != 3 {
		t.Fatalf("summary = %v", summary)
	}

	// Node assets.
	var nodeID, nodeStatus string
	if err := srv.pool.QueryRow(context.Background(), `
		SELECT a.id, a.metadata->'observed'->>'status'
		FROM assets a JOIN asset_types t ON t.id = a.type_id
		WHERE t.slug = 'k8s-node' AND a.name = 'node-1'`).Scan(&nodeID, &nodeStatus); err != nil {
		t.Fatalf("node asset: %v", err)
	}
	if nodeStatus != "online" {
		t.Fatalf("node-1 observed status = %q, want online", nodeStatus)
	}
	var offline string
	if err := srv.pool.QueryRow(context.Background(), `
		SELECT a.metadata->'observed'->>'status'
		FROM assets a JOIN asset_types t ON t.id = a.type_id
		WHERE t.slug = 'k8s-node' AND a.name = 'node-2'`).Scan(&offline); err != nil {
		t.Fatalf("node-2: %v", err)
	}
	if offline != "offline" {
		t.Fatalf("node-2 observed status = %q, want offline", offline)
	}

	// Pod asset, parented to its node.
	var podParent string
	if err := srv.pool.QueryRow(context.Background(), `
		SELECT a.parent_id::text
		FROM assets a JOIN asset_types t ON t.id = a.type_id
		WHERE t.slug = 'pod' AND a.name = 'default/nginx-1'`).Scan(&podParent); err != nil {
		t.Fatalf("pod asset: %v", err)
	}
	if podParent != nodeID {
		t.Fatalf("pod parent = %q, want node %q", podParent, nodeID)
	}

	// Services imported with endpoints.
	services := store.NewServices(srv.pool)
	svc, err := services.FindBySlug(context.Background(), "default-nginx")
	if err != nil {
		t.Fatalf("service import: %v", err)
	}
	if svc.DeploymentType != "kubernetes" {
		t.Fatalf("deployment_type = %q", svc.DeploymentType)
	}
	endpoints, err := services.ListEndpoints(context.Background(), svc.ID)
	if err != nil {
		t.Fatalf("endpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Port == nil || *endpoints[0].Port != 80 {
		t.Fatalf("endpoints = %+v", endpoints)
	}
	if endpoints[0].Protocol != "tcp" {
		t.Fatalf("endpoint protocol = %q", endpoints[0].Protocol)
	}
	if _, err := services.FindBySlug(context.Background(), "kube-system-kube-dns"); err != nil {
		t.Fatalf("second service missing: %v", err)
	}
	// NodePort services are external and carry a reachable URL built from the
	// node's internal IP plus the allocated node port.
	web, err := services.FindBySlug(context.Background(), "default-web")
	if err != nil {
		t.Fatalf("nodeport service missing: %v", err)
	}
	webEndpoints, err := services.ListEndpoints(context.Background(), web.ID)
	if err != nil || len(webEndpoints) != 1 {
		t.Fatalf("nodeport endpoints: %+v (%v)", webEndpoints, err)
	}
	if !webEndpoints[0].IsExternal {
		t.Fatalf("nodeport endpoint not external: %+v", webEndpoints[0])
	}
	if webEndpoints[0].URL != "http://10.0.0.11:30080" {
		t.Fatalf("nodeport url = %q", webEndpoints[0].URL)
	}

	// Re-sync must not duplicate or overwrite declared services.
	svc.Notes = "operator note"
	if err := services.Update(context.Background(), &svc); err != nil {
		t.Fatalf("update service: %v", err)
	}
	summary2, err := integrations.SyncKubernetes(context.Background(), srv.pool, integ, "k8s-token")
	if err != nil {
		t.Fatalf("re-sync: %v", err)
	}
	if summary2["services"] != 0 || summary2["services_skipped"] != 3 {
		t.Fatalf("re-sync summary = %v", summary2)
	}
	after, err := services.FindBySlug(context.Background(), "default-nginx")
	if err != nil {
		t.Fatalf("service after re-sync: %v", err)
	}
	if after.Notes != "operator note" {
		t.Fatalf("declared service was overwritten: %q", after.Notes)
	}

	// Bad token is rejected by the API and surfaces as an error.
	if _, err := integrations.SyncKubernetes(context.Background(), srv.pool, integ, "wrong"); err == nil {
		t.Fatalf("expected auth error")
	}
}
