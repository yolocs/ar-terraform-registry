package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/abcxyz/pkg/logging"
	"github.com/abcxyz/pkg/serving"
	"github.com/yolocs/ar-terraform-registry/pkg/model"
)

type Config struct {
	Port      string
	Providers model.ProviderStore
	Modules   model.ModuleStore
	Logger    *slog.Logger
}

type Registry struct {
	cfg    *Config
	mux    *http.ServeMux
	ps     model.ProviderStore
	ms     model.ModuleStore
	logger *slog.Logger
}

func New(cfg *Config) (*Registry, error) {
	reg := &Registry{
		cfg:    cfg,
		ps:     cfg.Providers,
		ms:     cfg.Modules,
		logger: cfg.Logger,
		mux:    http.NewServeMux(),
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
	reg.logger.DebugContext(r.Context(), "ServiceDiscovery", "headers", r.Header)

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
		system    = r.PathValue("system")
	)
	ctx := logging.WithLogger(r.Context(), reg.logger)

	versions, err := reg.ms.ListModuleVersions(ctx, namespace, name, system)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		reg.logger.ErrorContext(ctx, "ListModuleVersions", "error", err)
		return
	}

	resp := ModuleVersionsResponse{
		Modules: []ModuleVersionsResponseModule{{}},
	}
	for _, v := range versions {
		resp.Modules[0].Versions = append(resp.Modules[0].Versions, ModuleVersionsResponseModuleVersion{Version: v.Version})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		reg.logger.ErrorContext(ctx, "ModuleVersions", "error", err)
		return
	}
}

func (reg *Registry) ModuleDownload(w http.ResponseWriter, r *http.Request) {
	var (
		namespace = r.PathValue("namespace")
		name      = r.PathValue("name")
		system    = r.PathValue("system")
		version   = r.PathValue("version")
	)
	ctx := logging.WithLogger(r.Context(), reg.logger)

	v, err := reg.ms.GetModuleVersion(ctx, namespace, name, system, version)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		reg.logger.ErrorContext(ctx, "GetModuleVersion", "error", err)
		return
	}

	w.Header().Set("X-Terraform-Get", v.SourceURL)
	w.WriteHeader(http.StatusNoContent)
}

func (reg *Registry) ProviderVersions(w http.ResponseWriter, r *http.Request) {
	reg.logger.DebugContext(r.Context(), "ProviderVersions", "headers", r.Header)

	var (
		namespace = r.PathValue("namespace")
		name      = r.PathValue("name")
	)
	ctx := logging.WithLogger(r.Context(), reg.logger)

	vs, err := reg.ps.ListProviderVersions(ctx, namespace, name)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		reg.logger.ErrorContext(ctx, "ListProviderVersions", "error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(vs); err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		reg.logger.ErrorContext(ctx, "ListProviderVersions", "error", err)
		return
	}
}

func (reg *Registry) ProviderDownload(w http.ResponseWriter, r *http.Request) {
	reg.logger.DebugContext(r.Context(), "ProviderDownload", "headers", r.Header)

	var (
		namespace = r.PathValue("namespace")
		name      = r.PathValue("name")
		version   = r.PathValue("version")
		os        = r.PathValue("os")
		arch      = r.PathValue("arch")
	)
	ctx := logging.WithLogger(r.Context(), reg.logger)

	provider, err := reg.ps.GetProviderVersion(ctx, namespace, name, version, os, arch)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		reg.logger.ErrorContext(ctx, "GetProviderVersion", "error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(provider); err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		reg.logger.ErrorContext(ctx, "GetProviderVersion", "error", err)
		return
	}
}

func (reg *Registry) ProviderAssetDownload(w http.ResponseWriter, r *http.Request) {
	reg.logger.DebugContext(r.Context(), "ProviderAssetDownload", "headers", r.Header)

	var (
		namespace = r.PathValue("namespace")
		assetName = r.PathValue("assetName")
	)
	ctx := logging.WithLogger(r.Context(), reg.logger)

	fr, err := reg.ps.GetProviderAsset(ctx, namespace, assetName)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		reg.logger.ErrorContext(ctx, "GetProviderAsset", "error", err)
		return
	}
	defer fr.Close()

	written, err := io.Copy(w, fr)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		reg.logger.ErrorContext(ctx, "Copy asset", "error", err)
		return
	}

	reg.logger.DebugContext(ctx, "ProviderAssetDownload", "written", written)
}

func (reg *Registry) setupRoutes() {
	reg.mux.HandleFunc("/", reg.Index)
	reg.mux.HandleFunc("/health", reg.Health)
	reg.mux.HandleFunc("/.well-known/{name}", reg.ServiceDiscovery)
	reg.mux.HandleFunc("/v1/modules/{namespace}/{name}/{system}/versions", reg.ModuleVersions)
	reg.mux.HandleFunc("/v1/modules/{namespace}/{name}/{system}/{version}/download", reg.ModuleDownload)
	reg.mux.HandleFunc("/download/module/{namespace}/asset/{assetName}", reg.ProviderAssetDownload)
	reg.mux.HandleFunc("/v1/providers/{namespace}/{name}/versions", reg.ProviderVersions)
	reg.mux.HandleFunc("/v1/providers/{namespace}/{name}/{version}/download/{os}/{arch}", reg.ProviderDownload)
	reg.mux.HandleFunc("/download/provider/{namespace}/asset/{assetName}", reg.ProviderAssetDownload)
}
