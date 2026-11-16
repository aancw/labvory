package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aancw/lanvory/internal/dnsx"
	"github.com/aancw/lanvory/internal/httpx"
	"github.com/aancw/lanvory/internal/store"
)

// ---------- domains & subdomains (DNS inventory) ----------

var (
	domainNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
	labelRe      = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

var recordTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "MX": true,
	"TXT": true, "SRV": true, "CAA": true, "NS": true,
}

type domainPayload struct {
	Name      string  `json:"name"`
	Registrar string  `json:"registrar"`
	Notes     string  `json:"notes"`
	ExpiresOn *string `json:"expires_on"`
}

func validateDomainPayload(p *domainPayload) *httpxError {
	p.Name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(p.Name), "."))
	if p.Name == "" || len(p.Name) > 253 || !domainNameRe.MatchString(p.Name) {
		return errInvalid("invalid_name", "Domain must be a valid lower-case hostname (e.g. example.com).")
	}
	if len([]rune(p.Registrar)) > 120 {
		return errInvalid("invalid_registrar", "Registrar must be 120 characters or fewer.")
	}
	if len([]rune(p.Notes)) > 2000 {
		return errInvalid("invalid_notes", "Notes must be 2000 characters or fewer.")
	}
	if p.ExpiresOn != nil && *p.ExpiresOn != "" {
		if _, err := time.Parse("2006-01-02", *p.ExpiresOn); err != nil {
			return errInvalid("invalid_expiry", "Expiry must be a date (YYYY-MM-DD).")
		}
	}
	return nil
}

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	domains, err := s.domains.List(r.Context())
	if err != nil {
		s.logger.Error("list domains failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load domains.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"domains": domains})
}

func (s *Server) handleGetDomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "domainID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid domain id.", nil)
		return
	}
	domain, err := s.domains.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Domain not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("get domain failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load domain.", nil)
		return
	}
	subdomains, err := s.domains.ListSubdomains(r.Context(), id, domain.Name)
	if err != nil {
		s.logger.Error("list subdomains failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load subdomains.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"domain": domain, "subdomains": subdomains})
}

func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	var p domainPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateDomainPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	domain, err := s.domains.Create(r.Context(), p.Name, p.Registrar, p.Notes, p.ExpiresOn)
	if errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_domain", "This domain already exists.", nil)
		return
	}
	if err != nil {
		s.logger.Error("create domain failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create domain.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "domain", domain.ID, domain.Name, "created", nil)
	httpx.WriteJSON(w, http.StatusCreated, domain)
}

func (s *Server) handleUpdateDomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "domainID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid domain id.", nil)
		return
	}
	var p domainPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateDomainPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	domain, err := s.domains.Update(r.Context(), id, p.Name, p.Registrar, p.Notes, p.ExpiresOn)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Domain not found.", nil)
		return
	case errors.Is(err, store.ErrConflict):
		httpx.Error(w, r, http.StatusConflict, "duplicate_domain", "This domain already exists.", nil)
		return
	case err != nil:
		s.logger.Error("update domain failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update domain.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "domain", domain.ID, domain.Name, "updated", nil)
	httpx.WriteJSON(w, http.StatusOK, domain)
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "domainID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid domain id.", nil)
		return
	}
	err := s.domains.Delete(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Domain not found.", nil)
		return
	case err != nil:
		s.logger.Error("delete domain failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete domain.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "domain", id, "", "deleted", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

type subdomainPayload struct {
	Label       string  `json:"label"`
	RecordType  string  `json:"record_type"`
	Value       string  `json:"value"`
	TTL         *int    `json:"ttl"`
	IPAddressID *string `json:"ip_address_id"`
	ServiceID   *string `json:"service_id"`
}

func validateSubdomainPayload(p *subdomainPayload) *httpxError {
	p.Label = strings.ToLower(strings.TrimSpace(p.Label))
	p.Label = strings.TrimSuffix(strings.TrimSuffix(p.Label, "."), " ")
	if p.Label != "" && !labelRe.MatchString(p.Label) {
		return errInvalid("invalid_label", "Label must be a single DNS label (letters, digits, hyphens).")
	}
	p.RecordType = strings.ToUpper(strings.TrimSpace(p.RecordType))
	if p.RecordType == "" {
		p.RecordType = "A"
	}
	if !recordTypes[p.RecordType] {
		return errInvalid("invalid_record_type", "Record type must be one of A, AAAA, CNAME, MX, TXT, SRV, CAA, NS.")
	}
	p.Value = strings.TrimSpace(p.Value)
	if len(p.Value) > 512 {
		return errInvalid("invalid_value", "Value must be 512 characters or fewer.")
	}
	if (p.RecordType == "A" || p.RecordType == "AAAA") && p.Value != "" {
		ip := net.ParseIP(p.Value)
		if ip == nil {
			return errInvalid("invalid_value", "A/AAAA records need an IP address as the declared value.")
		}
		if p.RecordType == "A" && ip.To4() == nil {
			return errInvalid("invalid_value", "A records need an IPv4 address.")
		}
		if p.RecordType == "AAAA" && ip.To4() != nil {
			return errInvalid("invalid_value", "AAAA records need an IPv6 address.")
		}
	}
	if p.TTL != nil && (*p.TTL < 0 || *p.TTL > 2147483647) {
		return errInvalid("invalid_ttl", "TTL must be between 0 and 2147483647.")
	}
	if p.IPAddressID != nil && *p.IPAddressID != "" && !isUUID(*p.IPAddressID) {
		return errInvalid("invalid_ip", "Invalid IP address reference.")
	}
	if p.ServiceID != nil && *p.ServiceID != "" && !isUUID(*p.ServiceID) {
		return errInvalid("invalid_service", "Invalid service reference.")
	}
	return nil
}

func (s *Server) handleCreateSubdomain(w http.ResponseWriter, r *http.Request) {
	domainID := chi.URLParam(r, "domainID")
	if !isUUID(domainID) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid domain id.", nil)
		return
	}
	domain, err := s.domains.Get(r.Context(), domainID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Domain not found.", nil)
		return
	}
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load domain.", nil)
		return
	}
	var p subdomainPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateSubdomainPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	sd, err := s.domains.CreateSubdomain(r.Context(), domainID, domain.Name, subdomainInput(p))
	if errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_record",
			"A record with this label and type already exists on the domain.", nil)
		return
	}
	if err != nil {
		s.logger.Error("create subdomain failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create record.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "subdomain", sd.ID, sd.FQDN, "created", nil)
	httpx.WriteJSON(w, http.StatusCreated, sd)
}

func (s *Server) handleUpdateSubdomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "subdomainID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid record id.", nil)
		return
	}
	var p subdomainPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateSubdomainPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	existing, apex, err := s.domains.GetSubdomain(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Record not found.", nil)
		return
	}
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load record.", nil)
		return
	}
	sd, err := s.domains.UpdateSubdomain(r.Context(), id, apex, subdomainInput(p))
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Record not found.", nil)
		return
	case errors.Is(err, store.ErrConflict):
		httpx.Error(w, r, http.StatusConflict, "duplicate_record", "A record with this label and type already exists.", nil)
		return
	case err != nil:
		s.logger.Error("update subdomain failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update record.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "subdomain", sd.ID, sd.FQDN, "updated", nil)
	_ = existing
	httpx.WriteJSON(w, http.StatusOK, sd)
}

func (s *Server) handleDeleteSubdomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "subdomainID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid record id.", nil)
		return
	}
	err := s.domains.DeleteSubdomain(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Record not found.", nil)
		return
	case err != nil:
		s.logger.Error("delete subdomain failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete record.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "subdomain", id, "", "deleted", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func subdomainInput(p subdomainPayload) store.SubdomainInput {
	in := store.SubdomainInput{
		Label:      p.Label,
		RecordType: p.RecordType,
		Value:      p.Value,
		TTL:        p.TTL,
	}
	if p.IPAddressID != nil && *p.IPAddressID != "" {
		in.IPAddressID = p.IPAddressID
	}
	if p.ServiceID != nil && *p.ServiceID != "" {
		in.ServiceID = p.ServiceID
	}
	return in
}

// ---------- DNS resolution (observed state) ----------

type resolveResult struct {
	Subdomain store.Subdomain `json:"subdomain"`
	Drift     bool            `json:"drift"`
	Error     string          `json:"error,omitempty"`
}

func (s *Server) resolveOne(ctx context.Context, id string) resolveResult {
	sd, _, err := s.domains.GetSubdomain(ctx, id)
	if err != nil {
		return resolveResult{Error: err.Error()}
	}
	observed, err := dnsx.ResolveRecord(ctx, sd.FQDN, sd.RecordType)
	if err != nil {
		return resolveResult{Subdomain: sd, Error: err.Error()}
	}
	updated, err := s.domains.SetObserved(ctx, id, observed)
	if err != nil {
		return resolveResult{Subdomain: sd, Error: err.Error()}
	}
	return resolveResult{Subdomain: updated, Drift: dnsx.Drift(updated.Value, observed)}
}

func (s *Server) handleResolveSubdomain(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "subdomainID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid record id.", nil)
		return
	}
	if _, _, err := s.domains.GetSubdomain(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Record not found.", nil)
		return
	}
	res := s.resolveOne(r.Context(), id)
	if res.Error != "" && res.Subdomain.ID == "" {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Record not found.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (s *Server) handleResolveDomain(w http.ResponseWriter, r *http.Request) {
	domainID := chi.URLParam(r, "domainID")
	if !isUUID(domainID) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid domain id.", nil)
		return
	}
	domain, err := s.domains.Get(r.Context(), domainID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Domain not found.", nil)
		return
	}
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load domain.", nil)
		return
	}
	records, err := s.domains.ListSubdomains(r.Context(), domainID, domain.Name)
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load records.", nil)
		return
	}

	// Resolve concurrently (bounded) so dead records don't serialize the pass.
	type outcome struct{ drift, failed bool }
	results := make([]outcome, len(records))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, rec := range records {
		wg.Add(1)
		go func(i int, rec store.Subdomain) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res := s.resolveOne(r.Context(), rec.ID)
			results[i] = outcome{drift: res.Drift, failed: res.Error != ""}
		}(i, rec)
	}
	wg.Wait()

	resolved, drifted, failed := 0, 0, 0
	for _, res := range results {
		switch {
		case res.failed:
			failed++
		case res.drift:
			drifted++
			resolved++
		default:
			resolved++
		}
	}
	s.auditChange(r, currentUser(r), "domain", domain.ID, domain.Name, "resolved",
		map[string]any{"resolved": resolved, "drifted": drifted, "failed": failed})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"resolved": resolved, "drifted": drifted, "failed": failed, "total": len(records),
	})
}
