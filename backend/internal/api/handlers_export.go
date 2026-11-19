package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"

	"github.com/aancw/labvory/internal/httpx"
	"github.com/aancw/labvory/internal/store"
)

// writeCSV sets the download headers and hands back a writer that flushes
// through the encoding layer.
func writeCSV(w http.ResponseWriter, r *http.Request, filename string) *csv.Writer {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	return csv.NewWriter(w)
}

// derefFloat renders an optional float or "".
func derefFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// handleExportAssetsCSV streams the whole asset inventory as CSV with the
// same filters the inventory list accepts (minus pagination).
func (s *Server) handleExportAssetsCSV(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.AssetFilter{
		Q:           q.Get("q"),
		Category:    q.Get("category"),
		TypeID:      q.Get("type_id"),
		Status:      q.Get("status"),
		Environment: q.Get("environment"),
		LocationID:  q.Get("location_id"),
		ParentID:    q.Get("parent_id"),
		Sort:        "name",
		Order:       "asc",
		Limit:       100000,
	}
	assets, _, err := s.assets.List(r.Context(), f)
	if err != nil {
		s.logger.Error("export assets failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not export assets.", nil)
		return
	}

	wr := writeCSV(w, r, "labvory-assets.csv")
	header := []string{"id", "name", "hostname", "type", "category", "status", "lifecycle",
		"environment", "location", "parent", "manufacturer", "model", "serial_number",
		"os", "os_version", "cpu_model", "cpu_cores", "ram_gb", "storage_gb",
		"purchase_date", "purchase_cost", "warranty_expires", "source", "notes"}
	if err := wr.Write(header); err != nil {
		return
	}
	for _, a := range assets {
		row := []string{
			a.ID, a.Name, a.Hostname, a.TypeLabel, a.Category, a.Status, a.Lifecycle,
			a.Environment, derefStrPtr(a.LocationName), derefStrPtr(a.ParentName),
			a.Manufacturer, a.Model, a.SerialNumber, a.OSName, a.OSVersion, a.CPUModel,
			derefInt(a.CPUCores), derefFloat(a.RamGB), derefFloat(a.StorageGB),
			derefStrPtr(a.PurchaseDate), derefFloat(a.PurchaseCost), derefStrPtr(a.WarrantyExpires),
			a.Source, a.Notes,
		}
		if err := wr.Write(row); err != nil {
			return
		}
	}
	wr.Flush()
}

// handleExportIPsCSV streams tracked addresses as CSV.
func (s *Server) handleExportIPsCSV(w http.ResponseWriter, r *http.Request) {
	ips, _, err := s.ipAddresses.ListGlobal(r.Context(), r.URL.Query().Get("status"),
		r.URL.Query().Get("q"), false, 100000, 0)
	if err != nil {
		s.logger.Error("export ips failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not export addresses.", nil)
		return
	}

	wr := writeCSV(w, r, "labvory-ip-addresses.csv")
	header := []string{"address", "subnet_cidr", "status", "assignment_method",
		"hostname", "asset_name", "last_observed_at", "observed_conflict"}
	if err := wr.Write(header); err != nil {
		return
	}
	for _, ip := range ips {
		observed := ""
		if ip.LastObservedAt != nil {
			observed = ip.LastObservedAt.Format("2006-01-02 15:04:05")
		}
		row := []string{
			ip.Address, ip.SubnetCIDR, ip.Status, derefStrPtr(ip.Method),
			ip.Hostname, derefStrPtr(ip.AssetName), observed,
			strconv.FormatBool(ip.ObservedConflict),
		}
		if err := wr.Write(row); err != nil {
			return
		}
	}
	wr.Flush()
}

func derefStrPtr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefInt(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}
