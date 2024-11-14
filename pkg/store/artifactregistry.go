// Package store implements the provider/module store with Artifact Registry
// Generic repos.
package store

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	ar "cloud.google.com/go/artifactregistry/apiv1"
	arpb "cloud.google.com/go/artifactregistry/apiv1/artifactregistrypb"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"github.com/abcxyz/pkg/logging"

	"github.com/yolocs/ar-terraform-registry/pkg/model"
)

type Config struct {
	ProjectID              string
	Location               string
	ArtifactRegistryClient *ar.Client
	Downloader             *Downloader
}

type ArtifactRegistryGeneric struct {
	client     *ar.Client
	downloader *Downloader
	scope      string
}

func NewArtifactRegistryGeneric(cfg *Config) (*ArtifactRegistryGeneric, error) {
	return &ArtifactRegistryGeneric{
		client:     cfg.ArtifactRegistryClient,
		downloader: cfg.Downloader,
		scope:      fmt.Sprintf("projects/%s/locations/%s", cfg.ProjectID, cfg.Location),
	}, nil
}

func (a *ArtifactRegistryGeneric) ListProviderVersions(ctx context.Context, namespace string, name string) (*model.ProviderVersions, error) {
	logger := logging.FromContext(ctx)

	repo, pkg := namespace, name
	pageToken := ""
	var fullVersions []string

	for {
		req := &arpb.ListVersionsRequest{
			Parent:    fmt.Sprintf("%s/repositories/%s/packages/%s", a.scope, repo, pkg),
			PageSize:  1000,
			PageToken: pageToken,
		}
		iter := a.client.ListVersions(ctx, req)

		for v, err := range iter.All() {
			if err != nil {
				return nil, fmt.Errorf("failed to iterate over versions: %w", err)
			}
			logger.DebugContext(ctx, "ListProviderVersions found version", "version", v.Name)
			fullVersions = append(fullVersions, path.Base(v.Name))
		}

		if iter.PageInfo().Token == "" {
			break
		}
		pageToken = iter.PageInfo().Token
	}

	vs, err := mapVersions(fullVersions)
	if err != nil {
		logger.ErrorContext(ctx, "ListProviderVersions found unrecognized version names", "error", err)
	}

	return vs, nil
}

func (a *ArtifactRegistryGeneric) GetProviderVersion(ctx context.Context, namespace string, name string, version string, os string, arch string) (*model.Provider, error) {
	logger := logging.FromContext(ctx)
	repo, pkg, fullVer := namespace, name, fullVersion(version, os, arch)
	req := &arpb.ListFilesRequest{
		Parent:   fmt.Sprintf("%s/repositories/%s", a.scope, repo),
		Filter:   fmt.Sprintf(`owner="%s/repositories/%s/packages/%s"`, a.scope, repo, pkg),
		PageSize: 1000,
	}

	// We don't expect a lot files per version.
	var files []*arpb.File
	iter := a.client.ListFiles(ctx, req)
	for f, err := range iter.All() {
		if err != nil {
			return nil, fmt.Errorf("failed to iterate over files: %w", err)
		}
		files = append(files, f)
	}

	var providerBinName, shaSumName, shaSumSigName, gpgKeyName string
	namePrefix := providerFileNamePrefix(pkg, fullVer, version)

	for _, f := range files {
		logger.DebugContext(ctx, "GetProviderVersion found file", "file", f.Name)
		fn := path.Base(f.Name)

		switch fn {
		case namePrefix + fmt.Sprintf("_%s_%s.zip", os, arch):
			providerBinName = fn
		case namePrefix + "_SHA256SUMS":
			shaSumName = fn
		case namePrefix + "_SHA256SUMS.sig":
			shaSumSigName = fn
		case namePrefix + "_gpg-public-key.pem":
			gpgKeyName = fn
		}
	}

	if providerBinName == "" {
		return nil, fmt.Errorf("provider binary not found for %q", fullVer)
	}
	if shaSumName == "" {
		return nil, fmt.Errorf("SHA256SUMS not found for %q", fullVer)
	}
	if shaSumSigName == "" {
		return nil, fmt.Errorf("SHA256SUMS.sig not found for %q", fullVer)
	}

	shaSums, err := a.parseSHASumFile(ctx, repo, shaSumName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SHA256SUMS: %w", err)
	}

	keys, err := a.parseGPGKeys(ctx, repo, gpgKeyName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GPG keys: %w", err)
	}

	downloadUrl := fmt.Sprintf("/download/provider/%s/asset/%s", repo, providerBinName)
	SHASumURL := fmt.Sprintf("/download/provider/%s/asset/%s", repo, shaSumName)
	SHASumSigURL := fmt.Sprintf("/download/provider/%s/asset/%s", repo, shaSumSigName)

	shaSum, fileNameInSHASums, err := findSHA(shaSums, providerBinName)
	if err != nil {
		return nil, err
	}

	p := &model.Provider{
		Protocols:           []string{"5.0"},
		OS:                  os,
		Arch:                arch,
		Filename:            fileNameInSHASums,
		DownloadURL:         downloadUrl,
		SHASumsURL:          SHASumURL,
		SHASumsSignatureURL: SHASumSigURL,
		SHASum:              shaSum,
		SigningKeys:         model.SigningKeys{GPGPublicKeys: keys},
	}

	return p, nil
}

func (a *ArtifactRegistryGeneric) GetProviderAsset(ctx context.Context, repo string, fileName string) (io.ReadCloser, error) {
	u := fmt.Sprintf("%s/repositories/%s/files/%s:download", a.scope, repo, fileName)
	r, err := a.downloader.Download(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", fileName, err)
	}
	return r, nil
}

func (a *ArtifactRegistryGeneric) ListModuleVersions(ctx context.Context, namespace, name, system string) ([]*model.ModuleVersion, error) {
	logger := logging.FromContext(ctx)

	repo, pkg := namespace, modulePkg(name, system)
	pageToken := ""
	var vs []*model.ModuleVersion

	for {
		req := &arpb.ListVersionsRequest{
			Parent:    fmt.Sprintf("%s/repositories/%s/packages/%s", a.scope, repo, pkg),
			PageSize:  1000,
			PageToken: pageToken,
		}
		iter := a.client.ListVersions(ctx, req)

		for v, err := range iter.All() {
			if err != nil {
				return nil, fmt.Errorf("failed to iterate over versions: %w", err)
			}

			logger.DebugContext(ctx, "ListModuleVersions found version", "version", v.Name)

			version := path.Base(v.Name)
			vs = append(vs, &model.ModuleVersion{
				Version:   version,
				SourceURL: fmt.Sprintf("/download/module/%s/asset/%s", repo, moduleFileName(pkg, version)),
			})
		}

		if iter.PageInfo().Token == "" {
			break
		}
		pageToken = iter.PageInfo().Token
	}

	return vs, nil
}

func (a *ArtifactRegistryGeneric) GetModuleVersion(ctx context.Context, namespace, name, system, version string) (*model.ModuleVersion, error) {
	repo, pkg := namespace, modulePkg(name, system)
	return &model.ModuleVersion{
		Version:   version,
		SourceURL: fmt.Sprintf("/download/module/%s/asset/%s", repo, moduleFileName(pkg, version)),
	}, nil
}

func (a *ArtifactRegistryGeneric) parseSHASumFile(ctx context.Context, repo, fileName string) (map[string]string, error) {
	u := fmt.Sprintf("%s/repositories/%s/files/%s:download", a.scope, repo, fileName)
	r, err := a.downloader.Download(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", fileName, err)
	}
	defer r.Close()

	sums := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)

		hash := parts[0]
		fn := parts[1]

		sums[fn] = hash
	}
	return sums, nil
}

func (a *ArtifactRegistryGeneric) parseGPGKeys(ctx context.Context, namespace, fileName string) ([]model.GpgPublicKeys, error) {
	u := fmt.Sprintf("%s/repositories/%s/files/%s:download", a.scope, namespace, fileName)
	r, err := a.downloader.Download(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", fileName, err)
	}
	defer r.Close()

	all, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	els, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(all))
	if err != nil {
		return nil, err
	}

	if len(els) != 1 {
		return nil, fmt.Errorf("GPG Key contains %d entities, wanted 1", len(els))
	}

	key := els[0]
	return []model.GpgPublicKeys{{
		KeyID:          key.PrimaryKey.KeyIdString(),
		ASCIIArmor:     string(all),
		TrustSignature: "",
		Source:         "",
		SourceURL:      "",
	}}, nil
}

func findSHA(shaSums map[string]string, fileName string) (string, string, error) {
	for k, v := range shaSums {
		if strings.HasSuffix(fileName, k) {
			return v, k, nil
		}
	}
	return "", "", fmt.Errorf("failed to find SHA for %q", fileName)
}

func providerFileNamePrefix(pkg, fullVer, version string) string {
	return fmt.Sprintf("%s:%s:terraform-provider-%s_%s", pkg, fullVer, pkg, version)
}

func moduleFileName(pkg, version string) string {
	return fmt.Sprintf("%s:%s:module-archive.tar.gz", pkg, version)
}

func mapVersions(fullVersions []string) (*model.ProviderVersions, error) {
	var merr error
	m := make(map[string][]model.Platform)
	for _, v := range fullVersions {
		version, os, arch, err := parseFullVersion(v)
		if err != nil {
			merr = errors.Join(merr, err)
			continue
		}
		m[version] = append(m[version], model.Platform{
			OS:   os,
			Arch: arch,
		})
	}

	vs := &model.ProviderVersions{}
	for v, p := range m {
		vs.Versions = append(vs.Versions, model.ProviderVersion{
			Version:   v,
			Protocols: []string{"5.0"}, // Hard code for now.
			Platforms: p,
		})
	}

	return vs, merr
}

func fullVersion(version, os, arch string) string {
	return fmt.Sprintf("%s-%s-%s", version, os, arch)
}

func modulePkg(name, system string) string {
	return fmt.Sprintf("terraform-%s-%s", system, name)
}

func parseFullVersion(version string) (string, string, string, error) {
	parts := strings.Split(version, "-")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("invalid version format: %s", version)
	}
	return parts[0], parts[1], parts[2], nil
}
