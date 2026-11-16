package integrations

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"

	"github.com/petruknisme/lanvory/internal/store"
)

// kubeConfig is the connector configuration.
//
// Resolution order:
//  1. api_server (+ optional token secret) — explicit credentials
//  2. kubeconfig file path (+ optional context)
//  3. in-cluster service account (KUBERNETES_SERVICE_HOST)
type kubeConfig struct {
	Kubeconfig string `json:"kubeconfig"`
	Context    string `json:"context"`
	APIServer  string `json:"api_server"`
	InCluster  bool   `json:"in_cluster"`
	VerifyTLS  *bool  `json:"verify_tls"`
}

// SyncKubernetes imports cluster nodes, pods (as children of their node) and
// Services (into the service registry). Observed state only: declared fields
// are never rewritten.
func SyncKubernetes(ctx context.Context, pool *pgxpool.Pool, integ store.Integration, secretToken string) (map[string]any, error) {
	var cfg kubeConfig
	if len(integ.Config) > 0 {
		if err := json.Unmarshal(integ.Config, &cfg); err != nil {
			return nil, fmt.Errorf("invalid config: %w", err)
		}
	}
	client, err := newKubeClient(cfg, secretToken)
	if err != nil {
		return nil, err
	}

	var nodeList struct {
		Items []k8sNode `json:"items"`
	}
	if err := client.get(ctx, "/api/v1/nodes", &nodeList); err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	var podList struct {
		Items []k8sPod `json:"items"`
	}
	if err := client.get(ctx, "/api/v1/pods", &podList); err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	var svcList struct {
		Items []k8sService `json:"items"`
	}
	if err := client.get(ctx, "/api/v1/services", &svcList); err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	integrations := store.NewIntegrations(pool)
	now := time.Now().UTC().Format(time.RFC3339)
	nodeIDs := map[string]string{}
	nodeIPs := map[string]string{}
	created, updated := 0, 0

	for _, n := range nodeList.Items {
		ready := false
		for _, cond := range n.Status.Conditions {
			if cond.Type == "Ready" && strings.EqualFold(cond.Status, "True") {
				ready = true
			}
		}
		status := "offline"
		if ready {
			status = "online"
		}
		var roles []string
		for label := range n.Metadata.Labels {
			if rest, ok := strings.CutPrefix(label, "node-role.kubernetes.io/"); ok && rest != "" {
				roles = append(roles, rest)
			}
		}
		id, wasCreated, err := integrations.UpsertDiscoveredAsset(ctx, store.DiscoveredAsset{
			Name:          n.Metadata.Name,
			TypeSlug:      "k8s-node",
			Source:        "kubernetes",
			ExternalKey:   "k8s_node",
			ExternalValue: n.Metadata.Name,
			OSName:        n.Status.NodeInfo.OSImage,
			OSVersion:     n.Status.NodeInfo.KubeletVersion,
			CPUCores:      parseCPUCores(n.Status.Capacity["cpu"]),
			RamGB:         parseMemoryGi(n.Status.Capacity["memory"]),
			Metadata: map[string]any{
				"kubernetes": map[string]any{
					"roles":   roles,
					"kubelet": n.Status.NodeInfo.KubeletVersion,
					"runtime": n.Status.NodeInfo.ContainerRuntimeVersion,
				},
				"observed": map[string]any{"status": status, "checked_at": now},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", n.Metadata.Name, err)
		}
		if wasCreated {
			created++
		} else {
			updated++
		}
		nodeIDs[n.Metadata.Name] = id
		for _, addr := range n.Status.Addresses {
			if strings.EqualFold(addr.Type, "InternalIP") && addr.Address != "" {
				nodeIPs[n.Metadata.Name] = addr.Address
				break
			}
		}
	}

	// Any node can serve a NodePort; use the first known internal IP for links.
	anyNodeIP := ""
	for _, n := range nodeList.Items {
		if ip := nodeIPs[n.Metadata.Name]; ip != "" {
			anyNodeIP = ip
			break
		}
	}

	podCreated, podUpdated := 0, 0
	for _, p := range podList.Items {
		var images []string
		for _, c := range p.Spec.Containers {
			images = append(images, c.Image)
		}
		name := p.Metadata.Namespace + "/" + p.Metadata.Name
		id, wasCreated, err := integrations.UpsertDiscoveredAsset(ctx, store.DiscoveredAsset{
			Name:          name,
			TypeSlug:      "pod",
			Source:        "kubernetes",
			ExternalKey:   "k8s_pod",
			ExternalValue: p.Metadata.UID,
			Metadata: map[string]any{
				"kubernetes": map[string]any{
					"namespace": p.Metadata.Namespace,
					"node":      p.Spec.NodeName,
					"images":    images,
					"phase":     p.Status.Phase,
				},
				"observed": map[string]any{"status": strings.ToLower(p.Status.Phase), "checked_at": now},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("pod %s: %w", name, err)
		}
		if wasCreated {
			podCreated++
		} else {
			podUpdated++
		}
		if parent, ok := nodeIDs[p.Spec.NodeName]; ok {
			if err := integrations.EnsureParent(ctx, id, parent); err != nil {
				return nil, err
			}
		}
	}

	services := store.NewServices(pool)
	svcCreated, svcSkipped := 0, 0
	for _, s := range svcList.Items {
		if strings.EqualFold(s.Spec.Type, "ExternalName") {
			svcSkipped++
			continue
		}
		ns := s.Metadata.Namespace
		slug := kubernetesSlug(ns + "-" + s.Metadata.Name)
		if _, err := services.FindBySlug(ctx, slug); err == nil {
			// Declared services belong to the operator: never overwrite them.
			svcSkipped++
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}

		external := strings.EqualFold(s.Spec.Type, "LoadBalancer") || strings.EqualFold(s.Spec.Type, "NodePort")
		svc := store.Service{
			Name:           ns + "/" + s.Metadata.Name,
			Slug:           slug,
			Category:       "other",
			DeploymentType: "kubernetes",
			Environment:    "production",
			Status:         "online",
			Notes:          "Imported from Kubernetes (" + s.Spec.Type + " service, cluster IP " + s.Spec.ClusterIP + ").",
		}
		if err := services.Create(ctx, &svc); err != nil {
			return nil, fmt.Errorf("service %s: %w", svc.Name, err)
		}
		svcCreated++
		for _, port := range s.Spec.Ports {
			protocol := strings.ToLower(port.Protocol)
			if protocol != "udp" {
				protocol = "tcp"
			}
			p := port.Port
			label := port.Name
			if label == "" {
				label = fmt.Sprintf("%s/%d", protocol, port.Port)
			}
			if port.NodePort > 0 {
				label = fmt.Sprintf("%s (nodePort %d)", label, port.NodePort)
			}
			url := ""
			switch {
			case len(s.Status.LoadBalancer.Ingress) > 0:
				host := s.Status.LoadBalancer.Ingress[0].IP
				if host == "" {
					host = s.Status.LoadBalancer.Ingress[0].Hostname
				}
				if host != "" {
					url = fmt.Sprintf("http://%s:%d", host, p)
				}
			case port.NodePort > 0 && anyNodeIP != "":
				url = fmt.Sprintf("http://%s:%d", anyNodeIP, port.NodePort)
			}
			if err := services.CreateEndpoint(ctx, &store.ServiceEndpoint{
				ServiceID:  svc.ID,
				Label:      label,
				Protocol:   protocol,
				Port:       &p,
				URL:        url,
				IsExternal: external,
			}); err != nil {
				return nil, fmt.Errorf("service %s endpoint: %w", svc.Name, err)
			}
		}
	}

	return map[string]any{
		"nodes":            len(nodeList.Items),
		"pods":             len(podList.Items),
		"services":         svcCreated,
		"services_skipped": svcSkipped,
		"created":          created + podCreated,
		"updated":          updated + podUpdated,
	}, nil
}

// ---------- Kubernetes API payloads (subset) ----------

type k8sNode struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
		UID    string            `json:"uid"`
	} `json:"metadata"`
	Status struct {
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
		Capacity  map[string]string `json:"capacity"`
		Addresses []struct {
			Type    string `json:"type"`
			Address string `json:"address"`
		} `json:"addresses"`
		NodeInfo struct {
			KubeletVersion          string `json:"kubeletVersion"`
			ContainerRuntimeVersion string `json:"containerRuntimeVersion"`
			OSImage                 string `json:"osImage"`
		} `json:"nodeInfo"`
	} `json:"status"`
}

type k8sPod struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		UID       string `json:"uid"`
	} `json:"metadata"`
	Spec struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Name  string `json:"name"`
			Image string `json:"image"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

type k8sService struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		UID       string `json:"uid"`
	} `json:"metadata"`
	Spec struct {
		Type      string `json:"type"`
		ClusterIP string `json:"clusterIP"`
		Ports     []struct {
			Name     string `json:"name"`
			Protocol string `json:"protocol"`
			Port     int    `json:"port"`
			NodePort int    `json:"nodePort"`
		} `json:"ports"`
	} `json:"spec"`
	Status struct {
		LoadBalancer struct {
			Ingress []struct {
				IP       string `json:"ip"`
				Hostname string `json:"hostname"`
			} `json:"ingress"`
		} `json:"loadBalancer"`
	} `json:"status"`
}

// ---------- client ----------

type kubeClient struct {
	base  string
	token string
	hc    *http.Client
}

func (c *kubeClient) get(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s (check credentials/RBAC: needs list on nodes, pods and services)", resp.StatusCode, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func newKubeClient(cfg kubeConfig, secretToken string) (*kubeClient, error) {
	switch {
	case strings.TrimSpace(cfg.APIServer) != "":
		verify := cfg.VerifyTLS == nil || *cfg.VerifyTLS
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !verify}
		return &kubeClient{
			base:  strings.TrimSuffix(strings.TrimSpace(cfg.APIServer), "/"),
			token: secretToken,
			hc:    &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsCfg}},
		}, nil
	case strings.TrimSpace(cfg.Kubeconfig) != "":
		return clientFromKubeconfig(cfg)
	case cfg.InCluster || os.Getenv("KUBERNETES_SERVICE_HOST") != "":
		return clientInCluster(secretToken)
	default:
		return nil, errors.New("kubernetes connector needs a kubeconfig path, an API server (+ token), or in-cluster mode")
	}
}

const serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"

func clientInCluster(secretToken string) (*kubeClient, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, errors.New("in-cluster mode requires KUBERNETES_SERVICE_HOST/PORT")
	}
	token := secretToken
	if token == "" {
		raw, err := os.ReadFile(serviceAccountDir + "/token")
		if err != nil {
			return nil, fmt.Errorf("read service-account token: %w", err)
		}
		token = strings.TrimSpace(string(raw))
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if pool, err := certPoolFromFile(serviceAccountDir + "/ca.crt"); err == nil {
		tlsCfg.RootCAs = pool
	}
	return &kubeClient{
		base:  "https://" + host + ":" + port,
		token: token,
		hc:    &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsCfg}},
	}, nil
}

// kubeconfigFile is the subset of the kubeconfig schema the connector needs.
type kubeconfigFile struct {
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthority     string `yaml:"certificate-authority"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
			InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			Token                 string `yaml:"token"`
			TokenFile             string `yaml:"token-file"`
			ClientCertificate     string `yaml:"client-certificate"`
			ClientCertificateData string `yaml:"client-certificate-data"`
			ClientKey             string `yaml:"client-key"`
			ClientKeyData         string `yaml:"client-key-data"`
		} `yaml:"user"`
	} `yaml:"users"`
}

func clientFromKubeconfig(cfg kubeConfig) (*kubeClient, error) {
	raw, err := os.ReadFile(cfg.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig: %w", err)
	}
	var kc kubeconfigFile
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	contextName := cfg.Context
	if contextName == "" {
		contextName = kc.CurrentContext
	}
	var clusterName, userName string
	for _, c := range kc.Contexts {
		if c.Name == contextName {
			clusterName, userName = c.Context.Cluster, c.Context.User
		}
	}
	if clusterName == "" {
		return nil, fmt.Errorf("kubeconfig context %q not found", contextName)
	}

	var server, caData, caPath string
	insecure := false
	for _, c := range kc.Clusters {
		if c.Name == clusterName {
			server = c.Cluster.Server
			caData = c.Cluster.CertificateAuthorityData
			caPath = c.Cluster.CertificateAuthority
			insecure = c.Cluster.InsecureSkipTLSVerify
		}
	}
	if server == "" {
		return nil, fmt.Errorf("kubeconfig cluster %q has no server", clusterName)
	}

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.VerifyTLS != nil {
		insecure = !*cfg.VerifyTLS
	}
	tlsCfg.InsecureSkipVerify = insecure

	if !insecure {
		switch {
		case caData != "":
			if pool, err := certPoolFromBase64(caData); err == nil {
				tlsCfg.RootCAs = pool
			}
		case caPath != "":
			if pool, err := certPoolFromFile(caPath); err == nil {
				tlsCfg.RootCAs = pool
			}
		}
	}

	token := ""
	for _, u := range kc.Users {
		if u.Name != userName {
			continue
		}
		token = u.User.Token
		if token == "" && u.User.TokenFile != "" {
			if raw, err := os.ReadFile(u.User.TokenFile); err == nil {
				token = strings.TrimSpace(string(raw))
			}
		}
		certPEM, keyPEM := u.User.ClientCertificateData, u.User.ClientKeyData
		if certPEM == "" && u.User.ClientCertificate != "" {
			if raw, err := os.ReadFile(u.User.ClientCertificate); err == nil {
				certPEM = string(raw)
			}
		}
		if keyPEM == "" && u.User.ClientKey != "" {
			if raw, err := os.ReadFile(u.User.ClientKey); err == nil {
				keyPEM = string(raw)
			}
		}
		cert, err := keyPairFromDataOrPaths(certPEM, keyPEM)
		if err != nil {
			return nil, err
		}
		if cert != nil {
			tlsCfg.Certificates = []tls.Certificate{*cert}
		}
	}

	return &kubeClient{
		base:  strings.TrimSuffix(server, "/"),
		token: token,
		hc:    &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsCfg}},
	}, nil
}

func certPoolFromBase64(b64 string) (*x509.CertPool, error) {
	pem, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("no certificates found in CA data")
	}
	return pool, nil
}

func certPoolFromFile(path string) (*x509.CertPool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("no certificates found in CA file")
	}
	return pool, nil
}

func keyPairFromDataOrPaths(certData, keyData string) (*tls.Certificate, error) {
	if certData == "" || keyData == "" {
		return nil, nil
	}
	certPEM, err := decodeMaybeBase64(certData)
	if err != nil {
		return nil, err
	}
	keyPEM, err := decodeMaybeBase64(keyData)
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("client certificate: %w", err)
	}
	return &cert, nil
}

func decodeMaybeBase64(s string) ([]byte, error) {
	if strings.Contains(s, "-----BEGIN") {
		return []byte(s), nil
	}
	return base64.StdEncoding.DecodeString(s)
}

// ---------- quantity parsing ----------

func parseCPUCores(v string) *int {
	if v == "" {
		return nil
	}
	if n, err := strconv.Atoi(v); err == nil {
		return &n
	}
	if milli, ok := strings.CutSuffix(v, "m"); ok {
		if n, err := strconv.Atoi(milli); err == nil {
			cores := (n + 999) / 1000
			return &cores
		}
	}
	return nil
}

func parseMemoryGi(v string) *float64 {
	if v == "" {
		return nil
	}
	units := []struct {
		suffix string
		factor float64
	}{
		{"Ki", 1.0 / (1024 * 1024)}, {"Mi", 1.0 / 1024}, {"Gi", 1}, {"Ti", 1024},
	}
	for _, u := range units {
		if num, ok := strings.CutSuffix(v, u.suffix); ok {
			if n, err := strconv.ParseFloat(num, 64); err == nil {
				gb := n * u.factor
				return &gb
			}
		}
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		gb := n / (1024 * 1024 * 1024)
		return &gb
	}
	return nil
}

func kubernetesSlug(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
