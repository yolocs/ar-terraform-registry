package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Registry struct {
	cfg *Config
	mux *http.ServeMux
}

func New(cfg *Config) (*Registry, error) {
	reg := &Registry{
		cfg: cfg,
		mux: http.NewServeMux(),
	}
	reg.setupRoutes()
	return reg, nil
}

// Route handlers
func (reg *Registry) Index(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Terraform Registry")
}

func (reg *Registry) Health(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (reg *Registry) ServiceDiscovery(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// TODO: Implement proper service discovery response
	json.NewEncoder(w).Encode(map[string]interface{}{
		"service": name,
		"status":  "available",
	})
}

func (reg *Registry) ModuleVersions(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"modules": []map[string]interface{}{{
			"versions": []map[string]string{{
				"version": "1.0.0",
			}},
			"namespace": r.PathValue("namespace"),
			"name":      r.PathValue("name"),
			"provider":  r.PathValue("provider"),
		}},
	}
	json.NewEncoder(w).Encode(response)
}

func (reg *Registry) ModuleDownload(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Download module: %s/%s/%s/%s",
		r.PathValue("namespace"),
		r.PathValue("name"),
		r.PathValue("provider"),
		r.PathValue("version"))
}

func (reg *Registry) ProviderVersions(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"versions": []map[string]interface{}{{
			"version":   "1.0.0",
			"protocols": []string{"5.0"},
		}},
		"namespace": r.PathValue("namespace"),
		"name":      r.PathValue("name"),
	}
	json.NewEncoder(w).Encode(response)
}

func (reg *Registry) ProviderDownload(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Download provider: %s/%s/%s for %s/%s",
		r.PathValue("namespace"),
		r.PathValue("name"),
		r.PathValue("version"),
		r.PathValue("os"),
		r.PathValue("arch"))
}

func (reg *Registry) ProviderAssetDownload(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Download asset %s for provider: %s/%s/%s",
		r.PathValue("assetName"),
		r.PathValue("namespace"),
		r.PathValue("name"),
		r.PathValue("version"))
}

func (reg *Registry) setupRoutes() {
	reg.mux.Handle("GET /", http.HandlerFunc(reg.Index))
	reg.mux.Handle("GET /health", http.HandlerFunc(reg.Health))
	reg.mux.Handle("GET /.well-known/{name}", http.HandlerFunc(reg.ServiceDiscovery))
	reg.mux.Handle("GET /v1/modules/{namespace}/{name}/{provider}/versions", http.HandlerFunc(reg.ModuleVersions))
	reg.mux.Handle("GET /v1/modules/{namespace}/{name}/{provider}/{version}/download", http.HandlerFunc(reg.ModuleDownload))
	reg.mux.Handle("GET /v1/providers/{namespace}/{name}/versions", http.HandlerFunc(reg.ProviderVersions))
	reg.mux.Handle("GET /v1/providers/{namespace}/{name}/{version}/download/{os}/{arch}", http.HandlerFunc(reg.ProviderDownload))
	reg.mux.Handle("GET /download/provider/{namespace}/{name}/{version}/asset/{assetName}", http.HandlerFunc(reg.ProviderAssetDownload))
}
