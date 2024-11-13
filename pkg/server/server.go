package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/abcxyz/pkg/serving"
)

type Registry struct {
	cfg    *Config
	mux    *http.ServeMux
	logger *slog.Logger
}

func New(cfg *Config, logger *slog.Logger) (*Registry, error) {
	reg := &Registry{
		cfg:    cfg,
		mux:    http.NewServeMux(),
		logger: logger,
	}
	reg.setupRoutes()
	return reg, nil
}

// Start starsts the reigstry server. This will be a block call.
func (reg *Registry) Start(ctx context.Context) error {
	server, err := serving.New(reg.cfg.Port)
	if err != nil {
		return fmt.Errorf("failed to create serving infrastructure: %w", err)
	}

	if err := server.StartHTTPHandler(ctx, reg.mux); err != nil {
		return fmt.Errorf("failed to start HTTP handler: %w", err)
	}

	return nil
}

// Route handlers
func (reg *Registry) Index(w http.ResponseWriter, r *http.Request) {
	if _, err := w.Write([]byte("Terraform Registry based on GCP Artifact Registry\n")); err != nil {
		reg.logger.ErrorContext(r.Context(), "Index", "error", err)
	}
}

type HealthResponse struct {
	Status string `json:"status"`
}

func (reg *Registry) Health(w http.ResponseWriter, r *http.Request) {
	resp := HealthResponse{
		Status: "OK",
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	if err := enc.Encode(resp); err != nil {
		reg.logger.ErrorContext(r.Context(), "Health", "error", err)
	}
}

type ServiceDiscoveryResponse struct {
	ModulesV1   string `json:"modules.v1"`
	ProvidersV1 string `json:"providers.v1"`
}

func (reg *Registry) ServiceDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("name") != "terraform.json" {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	spec := ServiceDiscoveryResponse{
		ModulesV1:   "/v1/modules/",
		ProvidersV1: "/v1/providers/",
	}

	resp, err := json.Marshal(spec)
	if err != nil {
		reg.logger.ErrorContext(r.Context(), "ServiceDiscovery", "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(resp); err != nil {
		reg.logger.ErrorContext(r.Context(), "ServiceDiscovery", "error", err)
	}
}

type ModuleVersionsResponse struct {
	Modules []ModuleVersionsResponseModule `json:"modules"`
}

type ModuleVersionsResponseModule struct {
	Versions []ModuleVersionsResponseModuleVersion `json:"versions"`
}

type ModuleVersionsResponseModuleVersion struct {
	Version string `json:"version"`
}

func (reg *Registry) ModuleVersions(w http.ResponseWriter, r *http.Request) {
	var (
		namespace = r.PathValue("namespace")
		name      = r.PathValue("name")
		provider  = r.PathValue("provider")
	)

	response := map[string]interface{}{
		"modules": []map[string]interface{}{{
			"versions": []map[string]string{{
				"version": "1.0.0",
			}},
			"namespace": namespace,
			"name":      name,
			"provider":  provider,
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
	reg.mux.HandleFunc("/", reg.Index)
	reg.mux.HandleFunc("/health", reg.Health)
	reg.mux.HandleFunc("/.well-known/{name}", reg.ServiceDiscovery)
	reg.mux.HandleFunc("/v1/modules/{namespace}/{name}/{provider}/versions", reg.ModuleVersions)
	reg.mux.HandleFunc("/v1/modules/{namespace}/{name}/{provider}/{version}/download", reg.ModuleDownload)
	reg.mux.HandleFunc("/v1/providers/{namespace}/{name}/versions", reg.ProviderVersions)
	reg.mux.HandleFunc("/v1/providers/{namespace}/{name}/{version}/download/{os}/{arch}", reg.ProviderDownload)
	reg.mux.HandleFunc("/download/provider/{namespace}/{name}/{version}/asset/{assetName}", reg.ProviderAssetDownload)
}
