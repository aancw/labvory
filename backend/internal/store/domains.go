package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Domain is a DNS zone/apex owned by the operator (e.g. example.com).
type Domain struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Registrar  string    `json:"registrar"`
	ExpiresOn  *string   `json:"expires_on,omitempty"`
	Notes      string    `json:"notes"`
	Subdomains int       `json:"subdomain_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Subdomain is one DNS record under a domain. Declared values (Value) are
// operator-owned; ObservedValue is the last DNS answer.
type Subdomain struct {
	ID            string     `json:"id"`
	DomainID      string     `json:"domain_id"`
	Label         string     `json:"label"`
	FQDN          string     `json:"fqdn"`
	RecordType    string     `json:"record_type"`
	Value         string     `json:"value"`
	TTL           *int       `json:"ttl,omitempty"`
	IPAddressID   *string    `json:"ip_address_id,omitempty"`
	ServiceID     *string    `json:"service_id,omitempty"`
	ObservedValue string     `json:"observed_value"`
	ObservedAt    *time.Time `json:"observed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Domains is the DNS inventory store.
type Domains struct{ pool *pgxpool.Pool }

func NewDomains(pool *pgxpool.Pool) *Domains { return &Domains{pool: pool} }

const domainColumns = `id, name, registrar, expires_on, notes, created_at, updated_at`

func scanDomain(row pgx.Row) (Domain, error) {
	var d Domain
	var expires *time.Time
	err := row.Scan(&d.ID, &d.Name, &d.Registrar, &expires, &d.Notes, &d.CreatedAt, &d.UpdatedAt)
	if expires != nil {
		s := expires.Format("2006-01-02")
		d.ExpiresOn = &s
	}
	return d, err
}

// List returns all domains with subdomain counts.
func (s *Domains) List(ctx context.Context) ([]Domain, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+domainColumns+`,
		       (SELECT count(*) FROM subdomains sd WHERE sd.domain_id = domains.id)
		FROM domains ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var d Domain
		var expires *time.Time
		if err := rows.Scan(&d.ID, &d.Name, &d.Registrar, &expires, &d.Notes, &d.CreatedAt, &d.UpdatedAt, &d.Subdomains); err != nil {
			return nil, err
		}
		if expires != nil {
			v := expires.Format("2006-01-02")
			d.ExpiresOn = &v
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Get returns one domain by id.
func (s *Domains) Get(ctx context.Context, id string) (Domain, error) {
	d, err := scanDomain(s.pool.QueryRow(ctx, `SELECT `+domainColumns+` FROM domains WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	if err != nil {
		return Domain{}, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM subdomains WHERE domain_id=$1`, id).Scan(&d.Subdomains); err != nil {
		return Domain{}, err
	}
	return d, nil
}

// GetByName returns a domain by its apex name.
func (s *Domains) GetByName(ctx context.Context, name string) (Domain, error) {
	d, err := scanDomain(s.pool.QueryRow(ctx, `SELECT `+domainColumns+` FROM domains WHERE name=$1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	return d, err
}

// Create inserts a domain; duplicate names map to ErrConflict.
func (s *Domains) Create(ctx context.Context, name, registrar, notes string, expiresOn *string) (Domain, error) {
	d, err := scanDomain(s.pool.QueryRow(ctx, `
		INSERT INTO domains (name, registrar, notes, expires_on)
		VALUES ($1, $2, $3, $4::date)
		RETURNING `+domainColumns,
		name, registrar, notes, expiresOn))
	if err != nil {
		return Domain{}, mapConstraint(err)
	}
	return d, nil
}

// Update rewrites the editable domain fields.
func (s *Domains) Update(ctx context.Context, id, name, registrar, notes string, expiresOn *string) (Domain, error) {
	d, err := scanDomain(s.pool.QueryRow(ctx, `
		UPDATE domains SET name=$2, registrar=$3, notes=$4, expires_on=$5::date, updated_at=now()
		WHERE id=$1
		RETURNING `+domainColumns,
		id, name, registrar, notes, expiresOn))
	if errors.Is(err, pgx.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	if err != nil {
		return Domain{}, mapConstraint(err)
	}
	return d, nil
}

// Delete removes a domain and its subdomains.
func (s *Domains) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM domains WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const subdomainColumns = `id, domain_id, label, record_type, value, ttl, ip_address_id, service_id,
	observed_value, observed_at, created_at, updated_at`

func scanSubdomain(row pgx.Row, apex string) (Subdomain, error) {
	var sd Subdomain
	err := row.Scan(&sd.ID, &sd.DomainID, &sd.Label, &sd.RecordType, &sd.Value, &sd.TTL,
		&sd.IPAddressID, &sd.ServiceID, &sd.ObservedValue, &sd.ObservedAt,
		&sd.CreatedAt, &sd.UpdatedAt)
	sd.FQDN = FQDN(sd.Label, apex)
	return sd, err
}

// FQDN joins a label and apex ("", "example.com" → "example.com").
func FQDN(label, apex string) string {
	if label == "" {
		return apex
	}
	return label + "." + apex
}

// ListSubdomains returns the records of one domain.
func (s *Domains) ListSubdomains(ctx context.Context, domainID, apex string) ([]Subdomain, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+subdomainColumns+` FROM subdomains
		WHERE domain_id=$1 ORDER BY label, record_type`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subdomain{}
	for rows.Next() {
		sd, err := scanSubdomain(rows, apex)
		if err != nil {
			return nil, err
		}
		out = append(out, sd)
	}
	return out, rows.Err()
}

// GetSubdomain returns one record by id.
func (s *Domains) GetSubdomain(ctx context.Context, id string) (Subdomain, string, error) {
	var apex string
	var domainID string
	err := s.pool.QueryRow(ctx, `SELECT domain_id FROM subdomains WHERE id=$1`, id).Scan(&domainID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subdomain{}, "", ErrNotFound
	}
	if err != nil {
		return Subdomain{}, "", err
	}
	if err := s.pool.QueryRow(ctx, `SELECT name FROM domains WHERE id=$1`, domainID).Scan(&apex); err != nil {
		return Subdomain{}, "", err
	}
	sd, err := scanSubdomain(s.pool.QueryRow(ctx, `SELECT `+subdomainColumns+` FROM subdomains WHERE id=$1`, id), apex)
	return sd, apex, err
}

// SubdomainInput is the writable shape of a record.
type SubdomainInput struct {
	Label       string
	RecordType  string
	Value       string
	TTL         *int
	IPAddressID *string
	ServiceID   *string
}

// CreateSubdomain inserts a record; duplicates (same label+type) map to ErrConflict.
func (s *Domains) CreateSubdomain(ctx context.Context, domainID, apex string, in SubdomainInput) (Subdomain, error) {
	sd, err := scanSubdomain(s.pool.QueryRow(ctx, `
		INSERT INTO subdomains (domain_id, label, record_type, value, ttl, ip_address_id, service_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+subdomainColumns,
		domainID, in.Label, in.RecordType, in.Value, in.TTL, in.IPAddressID, in.ServiceID), apex)
	if err != nil {
		return Subdomain{}, mapConstraint(err)
	}
	return sd, nil
}

// UpdateSubdomain rewrites the editable record fields.
func (s *Domains) UpdateSubdomain(ctx context.Context, id, apex string, in SubdomainInput) (Subdomain, error) {
	sd, err := scanSubdomain(s.pool.QueryRow(ctx, `
		UPDATE subdomains SET label=$2, record_type=$3, value=$4, ttl=$5,
		       ip_address_id=$6, service_id=$7, updated_at=now()
		WHERE id=$1
		RETURNING `+subdomainColumns,
		id, in.Label, in.RecordType, in.Value, in.TTL, in.IPAddressID, in.ServiceID), apex)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subdomain{}, ErrNotFound
	}
	if err != nil {
		return Subdomain{}, mapConstraint(err)
	}
	return sd, nil
}

// DeleteSubdomain removes one record.
func (s *Domains) DeleteSubdomain(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM subdomains WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetObserved stores a DNS observation for a record.
func (s *Domains) SetObserved(ctx context.Context, id, observed string) (Subdomain, error) {
	var apex string
	var domainID string
	err := s.pool.QueryRow(ctx, `SELECT domain_id FROM subdomains WHERE id=$1`, id).Scan(&domainID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subdomain{}, ErrNotFound
	}
	if err != nil {
		return Subdomain{}, err
	}
	_ = s.pool.QueryRow(ctx, `SELECT name FROM domains WHERE id=$1`, domainID).Scan(&apex)
	sd, err := scanSubdomain(s.pool.QueryRow(ctx, `
		UPDATE subdomains SET observed_value=$2, observed_at=now() WHERE id=$1
		RETURNING `+subdomainColumns, id, observed), apex)
	return sd, err
}

// UpsertDiscovered inserts a record found by DNS enumeration. Existing rows
// keep their declared value; only observed_value is refreshed.
func (s *Domains) UpsertDiscovered(ctx context.Context, domainID, apex, label, recordType, observed string) (created bool, err error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO subdomains (domain_id, label, record_type, observed_value, observed_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (domain_id, label, record_type)
		DO UPDATE SET observed_value=EXCLUDED.observed_value, observed_at=now()`,
		domainID, label, recordType, observed)
	if err != nil {
		return false, err
	}
	// RowsAffected is 1 for inserts and updates alike; report whether the row
	// existed before by checking a fresh count is not possible — callers treat
	// this as "seen", which is what the summary needs.
	return tag.RowsAffected() > 0, nil
}
