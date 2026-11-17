package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aancw/lanvory/internal/httpx"
	"github.com/aancw/lanvory/internal/ipam"
	"github.com/aancw/lanvory/internal/store"
)

const gridMaxAddresses = 4096 // beyond this, clients use the paginated table

// ---------- network contexts ----------

type contextPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleListNetworkContexts(w http.ResponseWriter, r *http.Request) {
	items, err := s.networkContexts.List(r.Context())
	if err != nil {
		s.logger.Error("list contexts failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load contexts.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"contexts": items})
}

func (s *Server) handleCreateNetworkContext(w http.ResponseWriter, r *http.Request) {
	var p contextPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 60 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_name",
			"Context name must be 1-60 characters.", nil)
		return
	}
	c, err := s.networkContexts.Create(r.Context(), p.Name, slugifyName(p.Name), p.Description)
	if errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_context",
			"A context with this name already exists.", nil)
		return
	}
	if err != nil {
		s.logger.Error("create context failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create context.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "network_context", c.ID, c.Name, "created", nil)
	httpx.WriteJSON(w, http.StatusCreated, c)
}

func (s *Server) handleUpdateNetworkContext(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "contextID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid context id.", nil)
		return
	}
	var p contextPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	c, err := s.networkContexts.Update(r.Context(), id, strings.TrimSpace(p.Name), p.Description)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Context not found.", nil)
		return
	}
	if errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_context",
			"A context with this name already exists.", nil)
		return
	}
	if err != nil {
		s.logger.Error("update context failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update context.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "network_context", id, c.Name, "updated", nil)
	httpx.WriteJSON(w, http.StatusOK, c)
}

// ---------- VLANs ----------

type vlanPayload struct {
	ContextID   string `json:"context_id"`
	VlanID      int    `json:"vlan_id"`
	Name        string `json:"name"`
	Purpose     string `json:"purpose"`
	Description string `json:"description"`
}

func (s *Server) handleListVlans(w http.ResponseWriter, r *http.Request) {
	items, err := s.vlans.List(r.Context())
	if err != nil {
		s.logger.Error("list vlans failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load VLANs.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"vlans": items})
}

func (s *Server) handleCreateVlan(w http.ResponseWriter, r *http.Request) {
	var p vlanPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateVlanPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	if _, err := s.networkContexts.Get(r.Context(), p.ContextID); err != nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "unknown_context",
			"Referenced network context does not exist.", nil)
		return
	}

	v := store.Vlan{ContextID: p.ContextID, VlanID: p.VlanID, Name: p.Name, Purpose: p.Purpose, Description: p.Description}
	if err := s.vlans.Create(r.Context(), &v); errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_vlan",
			"This VLAN ID already exists in the selected context. Use separate contexts for overlapping segments.", nil)
		return
	} else if err != nil {
		s.logger.Error("create vlan failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create VLAN.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "vlan", v.ID, v.Name, "created",
		map[string]any{"vlan_id": v.VlanID, "context_id": v.ContextID})
	httpx.WriteJSON(w, http.StatusCreated, v)
}

func (s *Server) handleUpdateVlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "vlanID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid VLAN id.", nil)
		return
	}
	var p vlanPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateVlanPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	existing, err := s.vlans.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "VLAN not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("vlan update: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update VLAN.", nil)
		return
	}

	v := store.Vlan{ID: id, ContextID: existing.ContextID, VlanID: p.VlanID,
		Name: p.Name, Purpose: p.Purpose, Description: p.Description}
	if err := s.vlans.Update(r.Context(), &v); errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_vlan",
			"This VLAN ID already exists in the context.", nil)
		return
	} else if err != nil {
		s.logger.Error("update vlan failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update VLAN.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "vlan", id, v.Name, "updated", map[string]any{"vlan_id": v.VlanID})
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteVlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "vlanID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid VLAN id.", nil)
		return
	}
	v, err := s.vlans.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "VLAN not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("vlan delete: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete VLAN.", nil)
		return
	}
	if err := s.vlans.Delete(r.Context(), id); err != nil {
		s.logger.Error("delete vlan failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete VLAN.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "vlan", id, v.Name, "deleted", map[string]any{"vlan_id": v.VlanID})
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func validateVlanPayload(p *vlanPayload) *httpxError {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 60 {
		return errInvalid("invalid_name", "VLAN name must be 1-60 characters.")
	}
	if p.VlanID < 1 || p.VlanID > 4094 {
		return errInvalid("invalid_vlan_id", "VLAN ID must be between 1 and 4094.")
	}
	if !isUUID(p.ContextID) {
		return errInvalid("invalid_context", "A valid network context is required.")
	}
	return nil
}

// ---------- subnets ----------

type subnetPayload struct {
	CIDR        string   `json:"cidr"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ContextID   string   `json:"context_id"`
	VlanID      *string  `json:"vlan_id"`
	Gateway     *string  `json:"gateway"`
	DNSServers  []string `json:"dns_servers"`
	DHCPStart   *string  `json:"dhcp_start"`
	DHCPEnd     *string  `json:"dhcp_end"`
	LocationID  *string  `json:"location_id"`
	Environment string   `json:"environment"`
}

// validateSubnetPayload parses the CIDR, checks ranges and overlap within the context.
// It returns the parsed prefix (masked) for reuse by the handler.
func (s *Server) validateSubnetPayload(r *http.Request, p *subnetPayload, selfID string) (netip.Prefix, *httpxError) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 80 {
		return netip.Prefix{}, errInvalid("invalid_name", "Subnet name must be 1-80 characters.")
	}
	prefix, err := ipam.ParseCIDR(p.CIDR)
	if err != nil {
		return netip.Prefix{}, errInvalid("invalid_cidr", "CIDR must be valid network notation, e.g. 10.10.0.0/24.")
	}
	if !environments[p.Environment] {
		p.Environment = "production"
	}
	if !isUUID(p.ContextID) {
		return netip.Prefix{}, errInvalid("invalid_context", "A valid network context is required.")
	}
	if _, err := s.networkContexts.Get(r.Context(), p.ContextID); err != nil {
		return netip.Prefix{}, errInvalid("unknown_context", "Referenced network context does not exist.")
	}
	if p.VlanID != nil && *p.VlanID != "" {
		if !isUUID(*p.VlanID) {
			return netip.Prefix{}, errInvalid("invalid_vlan", "Invalid VLAN reference.")
		}
		vlan, err := s.vlans.Get(r.Context(), *p.VlanID)
		if err != nil {
			return netip.Prefix{}, errInvalid("unknown_vlan", "Referenced VLAN does not exist.")
		}
		if vlan.ContextID != p.ContextID {
			return netip.Prefix{}, errInvalid("vlan_context_mismatch",
				"The VLAN belongs to a different network context.")
		}
	} else {
		p.VlanID = nil
	}

	addr := func(field, value string) (netip.Addr, *httpxError) {
		if value == "" {
			return netip.Addr{}, nil
		}
		a, aerr := netip.ParseAddr(value)
		if aerr != nil {
			return netip.Addr{}, errInvalid("invalid_"+field, field+" is not a valid IP address.")
		}
		if herr := httpxFromIPAM(ipam.ValidateHostAddress(prefix, a)); herr != nil {
			return netip.Addr{}, herr
		}
		return a, nil
	}

	if _, herr := addr("gateway", derefStr(p.Gateway)); herr != nil {
		return netip.Prefix{}, herr
	} else {
		if derefStr(p.Gateway) == "" {
			p.Gateway = nil
		}
	}
	dhcpStart, herr := addr("dhcp_start", derefStr(p.DHCPStart))
	if herr != nil {
		return netip.Prefix{}, herr
	}
	dhcpEnd, herr := addr("dhcp_end", derefStr(p.DHCPEnd))
	if herr != nil {
		return netip.Prefix{}, herr
	}
	if (dhcpStart.IsValid()) != (dhcpEnd.IsValid()) {
		return netip.Prefix{}, errInvalid("invalid_dhcp_range", "DHCP range requires both start and end.")
	}
	if dhcpStart.IsValid() && dhcpStart.Compare(dhcpEnd) > 0 {
		return netip.Prefix{}, errInvalid("invalid_dhcp_range", "DHCP range start must precede end.")
	}
	if derefStr(p.DHCPStart) == "" {
		p.DHCPStart = nil
		p.DHCPEnd = nil
	}

	// Overlap is allowed only across network contexts (spec §C).
	existing, err := s.subnets.AllCIDRsInContext(r.Context(), p.ContextID, selfID)
	if err != nil {
		return netip.Prefix{}, &httpxError{http.StatusInternalServerError, "internal", "Could not check subnet overlap."}
	}
	parsed := make([]netip.Prefix, 0, len(existing))
	for _, c := range existing {
		cp, cerr := ipam.ParseCIDR(c)
		if cerr != nil {
			continue
		}
		parsed = append(parsed, cp)
	}
	if ipam.OverlapsAny(prefix, parsed) {
		return netip.Prefix{}, &httpxError{http.StatusConflict, "overlapping_subnet",
			"This subnet overlaps an existing subnet in the same context. Use a separate context for VRF-style separation."}
	}

	return prefix, nil
}

func httpxFromIPAM(err error) *httpxError {
	if err == nil {
		return nil
	}
	return errInvalid("invalid_address", err.Error())
}

func (s *Server) handleListSubnets(w http.ResponseWriter, r *http.Request) {
	items, err := s.subnets.List(r.Context(),
		r.URL.Query().Get("q"), r.URL.Query().Get("context_id"), r.URL.Query().Get("vlan_id"))
	if err != nil {
		s.logger.Error("list subnets failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load subnets.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"subnets": items})
}

func (s *Server) handleCreateSubnet(w http.ResponseWriter, r *http.Request) {
	var p subnetPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	prefix, herr := s.validateSubnetPayload(r, &p, "")
	if herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	_ = prefix

	n := store.Subnet{
		CIDR: p.CIDR, Name: p.Name, Description: p.Description,
		ContextID: p.ContextID, VlanID: p.VlanID, Gateway: p.Gateway,
		DNSServers: p.DNSServers, DHCPStart: p.DHCPStart, DHCPEnd: p.DHCPEnd,
		LocationID: p.LocationID, Environment: p.Environment,
	}
	if err := s.subnets.Create(r.Context(), &n); errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_subnet",
			"This CIDR already exists in the selected context.", nil)
		return
	} else if err != nil {
		s.logger.Error("create subnet failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create subnet.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "subnet", n.ID, n.Name, "created",
		map[string]any{"cidr": n.CIDR, "context_id": n.ContextID})
	s.writeSubnetDetail(w, r, n.ID, http.StatusCreated)
}

func (s *Server) handleUpdateSubnet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "subnetID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid subnet id.", nil)
		return
	}
	var p subnetPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	_, herr := s.validateSubnetPayload(r, &p, id)
	if herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}

	n := store.Subnet{
		ID: id, CIDR: p.CIDR, Name: p.Name, Description: p.Description,
		ContextID: p.ContextID, VlanID: p.VlanID, Gateway: p.Gateway,
		DNSServers: p.DNSServers, DHCPStart: p.DHCPStart, DHCPEnd: p.DHCPEnd,
		LocationID: p.LocationID, Environment: p.Environment,
	}
	if err := s.subnets.Update(r.Context(), &n); errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_subnet",
			"This CIDR already exists in the selected context.", nil)
		return
	} else if err != nil {
		s.logger.Error("update subnet failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update subnet.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "subnet", id, n.Name, "updated", map[string]any{"cidr": n.CIDR})
	s.writeSubnetDetail(w, r, id, http.StatusOK)
}

func (s *Server) handleDeleteSubnet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "subnetID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid subnet id.", nil)
		return
	}
	n, err := s.subnets.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Subnet not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("subnet delete: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete subnet.", nil)
		return
	}
	if err := s.subnets.Delete(r.Context(), id); errors.Is(err, store.ErrHasChildren) {
		httpx.Error(w, r, http.StatusConflict, "has_allocations",
			"This subnet still has tracked addresses. Release them before deleting.", nil)
		return
	} else if err != nil {
		s.logger.Error("delete subnet failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete subnet.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "subnet", id, n.Name, "deleted", map[string]any{"cidr": n.CIDR})
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleGetSubnet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "subnetID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid subnet id.", nil)
		return
	}
	s.writeSubnetDetail(w, r, id, http.StatusOK)
}

func (s *Server) writeSubnetDetail(w http.ResponseWriter, r *http.Request, id string, status int) {
	ctx := r.Context()
	subnet, err := s.subnets.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Subnet not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("get subnet failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load subnet.", nil)
		return
	}
	addresses, err := s.ipAddresses.ListBySubnet(ctx, id, r.URL.Query().Get("status"), r.URL.Query().Get("q"))
	if err != nil {
		s.logger.Error("subnet addresses failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load allocations.", nil)
		return
	}
	history, err := s.subnetsHistory(ctx, id, 20)
	if err != nil {
		s.logger.Error("subnet history failed", "error", err)
	}
	httpx.WriteJSON(w, status, map[string]any{
		"subnet":     subnet,
		"addresses":  addresses,
		"history":    history,
		"grid_limit": gridMaxAddresses,
	})
}

// subnetsHistory returns recent audit events for the subnet entity.
func (s *Server) subnetsHistory(ctx context.Context, id string, limit int) ([]store.AuditEvent, error) {
	return s.audit.ListForEntity(ctx, "subnet", id, limit)
}

// ---------- address grid & allocation ----------

type gridCell struct {
	Address  string  `json:"address"`
	Status   string  `json:"status"`
	IPID     *string `json:"ip_id,omitempty"`
	Hostname string  `json:"hostname,omitempty"`
}

type addressGrid struct {
	CIDR      string     `json:"cidr"`
	Usable    uint64     `json:"usable"`
	First     string     `json:"first"`
	Last      string     `json:"last"`
	Cells     []gridCell `json:"cells"`
	Truncated bool       `json:"truncated"`
	NextAvail []string   `json:"next_available"`
}

// handleAddressGrid builds the interactive allocation view for a subnet.
// Large prefixes (> gridMaxAddresses usable) are reported truncated.
func (s *Server) handleAddressGrid(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "subnetID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid subnet id.", nil)
		return
	}
	subnet, err := s.subnets.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Subnet not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("grid: subnet load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load subnet.", nil)
		return
	}

	prefix, err := ipam.ParseCIDR(subnet.CIDR)
	if err != nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_cidr",
			"Stored CIDR is not parseable: "+subnet.CIDR, nil)
		return
	}

	taken, err := s.ipAddresses.TakenAddresses(r.Context(), id)
	if err != nil {
		s.logger.Error("grid: taken addresses failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load allocations.", nil)
		return
	}

	rows, err := s.ipAddresses.ListBySubnet(r.Context(), id, "", "")
	if err != nil {
		s.logger.Error("grid: rows failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load allocations.", nil)
		return
	}
	byAddress := map[string]store.IPAddress{}
	for _, ip := range rows {
		byAddress[ip.Address] = ip
	}

	usable := ipam.UsableCount(prefix)
	truncated := usable > gridMaxAddresses
	cells := []gridCell{}
	if !truncated {
		first, last := ipam.UsableRange(prefix)
		cur := first
		for i := uint64(0); i < usable; i++ {
			addrStr := cur.String()
			cell := gridCell{Address: addrStr, Status: "free"}
			if ip, ok := byAddress[addrStr]; ok {
				cell.Status = ip.Status
				s := ip.ID
				cell.IPID = &s
				cell.Hostname = ip.Hostname
			}
			cells = append(cells, cell)
			if cur == last {
				break
			}
			cur = cur.Next()
			if !cur.IsValid() {
				break
			}
		}
	}

	next := ipam.NextAvailable(prefix, taken, 10)

	first, last := ipam.UsableRange(prefix)
	httpx.WriteJSON(w, http.StatusOK, addressGrid{
		CIDR: subnet.CIDR, Usable: usable, First: first.String(), Last: last.String(),
		Cells: cells, Truncated: truncated, NextAvail: next,
	})
}

type allocatePayload struct {
	Address     string `json:"address"` // empty → auto-assign next available
	Status      string `json:"status"`  // assigned | reserved | excluded
	Method      string `json:"assignment_method"`
	Hostname    string `json:"hostname"`
	Description string `json:"description"`
	InterfaceID string `json:"interface_id"`
	AssetID     string `json:"asset_id"`
	Auto        bool   `json:"auto"`
}

// handleAllocateIP implements the assign-a-new-address workflow.
func (s *Server) handleAllocateIP(w http.ResponseWriter, r *http.Request) {
	subnetID := chi.URLParam(r, "subnetID")
	if !isUUID(subnetID) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid subnet id.", nil)
		return
	}
	var p allocatePayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	subnet, err := s.subnets.Get(r.Context(), subnetID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Subnet not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("allocate: subnet load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not allocate address.", nil)
		return
	}

	prefix, perr := ipam.ParseCIDR(subnet.CIDR)
	if perr != nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_cidr", "Stored CIDR is not parseable.", nil)
		return
	}

	if !setOf("assigned", "reserved", "excluded")[p.Status] {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_status",
			"Allocation status must be assigned, reserved or excluded.", nil)
		return
	}
	if p.Status == "assigned" && p.Method == "" {
		p.Method = "static"
	}
	if p.Method != "" && !setOf("static", "dhcp", "reserved")[p.Method] {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_method",
			"Assignment method must be static, dhcp or reserved.", nil)
		return
	}

	// Interface/asset consistency: an interface must belong to an existing asset.
	assetID := p.AssetID
	if p.InterfaceID != "" {
		if !isUUID(p.InterfaceID) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_interface", "Invalid interface reference.", nil)
			return
		}
		rows, _, err := s.interfaces.ListAll(r.Context(), 300, 0)
		if err != nil {
			s.logger.Error("allocate: interfaces failed", "error", err)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not allocate address.", nil)
			return
		}
		var found *store.NetworkInterface
		for i := range rows {
			if rows[i].ID == p.InterfaceID {
				found = &rows[i]
				break
			}
		}
		if found == nil {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "unknown_interface",
				"Referenced interface does not exist.", nil)
			return
		}
		assetID = found.AssetID
		if p.AssetID != "" && p.AssetID != assetID {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "interface_asset_mismatch",
				"The interface belongs to a different asset than asset_id.", nil)
			return
		}
	}

	// Resolve the target address.
	address := strings.TrimSpace(p.Address)
	if address == "" {
		taken, err := s.ipAddresses.TakenAddresses(r.Context(), subnetID)
		if err != nil {
			s.logger.Error("allocate: taken failed", "error", err)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not allocate address.", nil)
			return
		}
		next := ipam.NextAvailable(prefix, taken, 1)
		if len(next) == 0 {
			httpx.Error(w, r, http.StatusConflict, "subnet_exhausted",
				"No available addresses left in this subnet.", nil)
			return
		}
		address = next[0]
		p.Auto = true
	}

	addr, aerr := netip.ParseAddr(address)
	if aerr != nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_address",
			"Address is not valid IP notation.", nil)
		return
	}
	if herr := httpxFromIPAM(ipam.ValidateHostAddress(prefix, addr)); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}

	ip := store.IPAddress{
		SubnetID: subnetID, Address: address, Status: p.Status,
		Method: stringPtr(p.Method), Hostname: strings.TrimSpace(p.Hostname),
		Description: strings.TrimSpace(p.Description),
		InterfaceID: stringPtr(p.InterfaceID), AssetID: stringPtr(assetID),
	}
	if err := s.ipAddresses.Allocate(r.Context(), &ip, assetID, currentUser(r).ID); errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "address_taken",
			"This address is already allocated with an active status.", nil)
		return
	} else if err != nil {
		s.logger.Error("allocate failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not allocate address.", nil)
		return
	}

	action := "allocated"
	if p.Auto {
		action = "allocated_auto"
	}
	s.auditChange(r, currentUser(r), "ip_address", ip.ID, address, action,
		map[string]any{"subnet": subnet.CIDR, "status": ip.Status, "asset_id": assetID,
			"interface_id": p.InterfaceID, "hostname": ip.Hostname})
	httpx.WriteJSON(w, http.StatusCreated, ip)
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// derefStr mirrors store.deref for nullable strings.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// slugifyName produces URL-safe slugs from names.
func slugifyName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "context"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}

type ipUpdatePayload struct {
	Status      string `json:"status"`
	Method      string `json:"assignment_method"`
	Hostname    string `json:"hostname"`
	Description string `json:"description"`
	InterfaceID string `json:"interface_id"`
}

func (s *Server) handleUpdateIP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "ipID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid address id.", nil)
		return
	}
	var p ipUpdatePayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if !setOf("available", "assigned", "reserved", "excluded", "conflict")[p.Status] {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_status",
			"Status must be available, assigned, reserved, excluded or conflict.", nil)
		return
	}

	existing, err := s.ipAddresses.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Address not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("ip update: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update address.", nil)
		return
	}

	ip := store.IPAddress{
		ID: id, SubnetID: existing.SubnetID, Address: existing.Address,
		Status: p.Status, Method: stringPtr(p.Method), Hostname: p.Hostname,
		Description: p.Description, InterfaceID: stringPtr(p.InterfaceID),
		AssetID: existing.AssetID,
	}
	if err := s.ipAddresses.Update(r.Context(), &ip, currentUser(r).ID); err != nil {
		s.logger.Error("update ip failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update address.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "ip_address", id, existing.Address, "updated",
		map[string]any{"from": existing.Status, "to": p.Status, "hostname": p.Hostname})
	httpx.WriteJSON(w, http.StatusOK, ip)
}

func (s *Server) handleReleaseIP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "ipID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid address id.", nil)
		return
	}
	existing, err := s.ipAddresses.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Address not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("release: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not release address.", nil)
		return
	}

	ip := store.IPAddress{
		ID: id, SubnetID: existing.SubnetID, Address: existing.Address,
		Status: "available", Hostname: existing.Hostname, Description: existing.Description,
	}
	if err := s.ipAddresses.Update(r.Context(), &ip, currentUser(r).ID); err != nil {
		s.logger.Error("release failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not release address.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "ip_address", id, existing.Address, "released",
		map[string]any{"subnet": existing.SubnetCIDR})
	httpx.WriteJSON(w, http.StatusOK, ip)
}

func (s *Server) handleDeleteIP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "ipID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid address id.", nil)
		return
	}
	existing, err := s.ipAddresses.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Address not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("ip delete: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete address.", nil)
		return
	}
	if err := s.ipAddresses.Delete(r.Context(), id); err != nil {
		s.logger.Error("delete ip failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete address.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "ip_address", id, existing.Address, "deleted", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleIPHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "ipID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid address id.", nil)
		return
	}
	events, err := s.ipAddresses.History(r.Context(), id)
	if err != nil {
		s.logger.Error("ip history failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load history.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"history": events})
}

// handleNextAvailable returns candidate free addresses for quick allocation.
func (s *Server) handleNextAvailable(w http.ResponseWriter, r *http.Request) {
	subnetID := chi.URLParam(r, "subnetID")
	if !isUUID(subnetID) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid subnet id.", nil)
		return
	}
	count := clampInt(parseQueryInt(r, "count", 10), 1, 100)
	subnet, err := s.subnets.Get(r.Context(), subnetID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Subnet not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("next-available: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not find addresses.", nil)
		return
	}
	prefix, perr := ipam.ParseCIDR(subnet.CIDR)
	if perr != nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_cidr", "Stored CIDR is not parseable.", nil)
		return
	}
	taken, err := s.ipAddresses.TakenAddresses(r.Context(), subnetID)
	if err != nil {
		s.logger.Error("next-available: taken failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not find addresses.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"addresses": ipam.NextAvailable(prefix, taken, count),
	})
}

// ---------- bulk operations on addresses ----------

type ipBulkPayload struct {
	Action      string `json:"action"` // reserve | exclude | release
	Start       string `json:"start"`
	End         string `json:"end"`   // inclusive; empty → single address (start)
	Count       int    `json:"count"` // alternative to End
	Hostname    string `json:"hostname"`
	Description string `json:"description"`
}

// handleBulkIP reserves/excludes/releases a contiguous range in one subnet.
func (s *Server) handleBulkIP(w http.ResponseWriter, r *http.Request) {
	subnetID := chi.URLParam(r, "subnetID")
	if !isUUID(subnetID) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid subnet id.", nil)
		return
	}
	var p ipBulkPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	subnet, err := s.subnets.Get(r.Context(), subnetID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Subnet not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("bulk ip: subnet load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Bulk operation failed.", nil)
		return
	}
	prefix, perr := ipam.ParseCIDR(subnet.CIDR)
	if perr != nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_cidr", "Stored CIDR is not parseable.", nil)
		return
	}

	start, serr := netip.ParseAddr(strings.TrimSpace(p.Start))
	if serr != nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_address", "Start address is not valid.", nil)
		return
	}
	end := start
	if strings.TrimSpace(p.End) != "" {
		end, serr = netip.ParseAddr(strings.TrimSpace(p.End))
		if serr != nil {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_address", "End address is not valid.", nil)
			return
		}
	} else if p.Count > 1 {
		e := start
		for i := 1; i < p.Count && e.IsValid(); i++ {
			e = e.Next()
		}
		end = e
	}

	// Every address in the range must be inside the subnet usable range.
	if herr := httpxFromIPAM(ipam.ValidateHostAddress(prefix, start)); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	if herr := httpxFromIPAM(ipam.ValidateHostAddress(prefix, end)); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}

	hosts, err := ipam.RangeHosts(start, end, gridMaxAddresses)
	if err != nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_range", err.Error(), nil)
		return
	}

	user := currentUser(r)
	var affected int
	switch p.Action {
	case "reserve", "exclude":
		status := "reserved"
		if p.Action == "exclude" {
			status = "excluded"
		}
		for _, h := range hosts {
			ip := store.IPAddress{
				SubnetID: subnetID, Address: h, Status: status,
				Method: stringPtr("reserved"), Hostname: p.Hostname, Description: p.Description,
			}
			if err := s.ipAddresses.Allocate(r.Context(), &ip, "", user.ID); err == nil {
				affected++
			} else if !errors.Is(err, store.ErrConflict) {
				s.logger.Error("bulk reserve failed", "error", err, "address", h)
				httpx.Error(w, r, http.StatusInternalServerError, "internal", "Bulk operation failed.", nil)
				return
			}
			// Conflicts (already allocated with active status) are skipped.
		}
	case "release":
		for _, h := range hosts {
			ip, err := s.ipAddresses.GetByAddress(r.Context(), subnetID, h)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				s.logger.Error("bulk release failed", "error", err)
				httpx.Error(w, r, http.StatusInternalServerError, "internal", "Bulk operation failed.", nil)
				return
			}
			if ip.Status == "available" {
				continue
			}
			free := store.IPAddress{ID: ip.ID, SubnetID: subnetID, Address: h, Status: "available"}
			if err := s.ipAddresses.Update(r.Context(), &free, user.ID); err != nil {
				s.logger.Error("bulk release update failed", "error", err)
				httpx.Error(w, r, http.StatusInternalServerError, "internal", "Bulk operation failed.", nil)
				return
			}
			affected++
		}
	default:
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_action",
			"Supported actions: reserve, exclude, release.", nil)
		return
	}

	s.auditChange(r, user, "ip_address", "", subnet.CIDR, "bulk_"+p.Action,
		map[string]any{"range": hosts[0] + "-" + hosts[len(hosts)-1], "affected": affected})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "affected": affected})
}

// handleListIPs serves the global tracked-address table with filters.
func (s *Server) handleListIPs(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(parseQueryInt(r, "limit", 100), 1, 300)
	offset := clampInt(parseQueryInt(r, "offset", 0), 0, 100000)
	observedConflict := r.URL.Query().Get("observed_conflict") == "true" ||
		r.URL.Query().Get("observed_conflict") == "1"
	items, total, err := s.ipAddresses.ListGlobal(r.Context(),
		r.URL.Query().Get("status"), r.URL.Query().Get("q"), observedConflict, limit, offset)
	if err != nil {
		s.logger.Error("list ips failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load addresses.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"addresses": items, "total": total, "limit": limit, "offset": offset,
	})
}

type ipIDsPayload struct {
	IDs []string `json:"ids"`
}

// handleReleaseIPsBulk releases the selected addresses (any non-available
// status) and closes their assignment history rows.
func (s *Server) handleReleaseIPsBulk(w http.ResponseWriter, r *http.Request) {
	var p ipIDsPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if len(p.IDs) == 0 || len(p.IDs) > 1000 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_request",
			"Provide between 1 and 1000 address ids.", nil)
		return
	}
	for _, id := range p.IDs {
		if !isUUID(id) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_request",
				"Invalid address id in selection.", nil)
			return
		}
	}
	released, err := s.ipAddresses.ReleaseMany(r.Context(), p.IDs)
	if err != nil {
		s.logger.Error("bulk release failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Bulk release failed.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "ip_address", "", fmt.Sprintf("%d addresses", released), "released_bulk",
		map[string]any{"released": released, "ids": p.IDs})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "released": released})
}

// handleDeleteIPsBulk removes the selected tracked addresses (assignment
// history cascades with the rows).
func (s *Server) handleDeleteIPsBulk(w http.ResponseWriter, r *http.Request) {
	var p ipIDsPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if len(p.IDs) == 0 || len(p.IDs) > 1000 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_request",
			"Provide between 1 and 1000 address ids.", nil)
		return
	}
	for _, id := range p.IDs {
		if !isUUID(id) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_request",
				"Invalid address id in selection.", nil)
			return
		}
	}
	deleted, err := s.ipAddresses.DeleteMany(r.Context(), p.IDs)
	if err != nil {
		s.logger.Error("bulk delete failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Bulk delete failed.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "ip_address", "", fmt.Sprintf("%d addresses", deleted), "deleted_bulk",
		map[string]any{"deleted": deleted, "ids": p.IDs})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
}
