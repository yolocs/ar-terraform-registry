// Package store implements the provider/module store with Artifact Registry
// Generic repos.
//
// Provider mapping to AR concept:
// - Namespace_name => Package
// - {V}_{OS}_{Arch} => Version
// - Provider binary => File under the version
// - SHASUM => File under the version
// - SHASUM.sig => File under the version
//
// Module mapping to AR concept:
// - Namespace_name => Package
// - Version => Version
// - Module zip => File under the version
package store

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	ar "cloud.google.com/go/artifactregistry/apiv1"
	arpb "cloud.google.com/go/artifactregistry/apiv1/artifactregistrypb"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"github.com/abcxyz/pkg/logging"

	"github.com/yolocs/ar-terraform-registry/pkg/model"
)

type Config struct {
	ProjectID string
	Location  string
}

type ArtifactRegistryGeneric struct {
	client     *ar.Client
	downloader *Downloader
	cfg        *Config
}

func NewArtifactRegistryGeneric(client *ar.Client, downloader *Downloader, cfg *Config) (*ArtifactRegistryGeneric, error) {
	return &ArtifactRegistryGeneric{
		client:     client,
		downloader: downloader,
		cfg:        cfg,
	}, nil
}

func (a *ArtifactRegistryGeneric) ListProviderVersions(ctx context.Context, namespace string, name string) (*model.ProviderVersions, error) {
	logger := logging.FromContext(ctx)

	pageToken := ""
	var rawVersions []string

	for {
		req := &arpb.ListVersionsRequest{
			Parent:    fmt.Sprintf("%s/repositories/%s/packages/%s", a.scope(), namespace, name),
			PageSize:  1000,
			PageToken: pageToken,
		}
		iter := a.client.ListVersions(ctx, req)

		for v, err := range iter.All() {
			if err != nil {
				return nil, fmt.Errorf("failed to iterate over versions: %w", err)
			}
			rawVersions = append(rawVersions, v.Name)
		}

		if iter.PageInfo().Token == "" {
			break
		}
		pageToken = iter.PageInfo().Token
	}

	vs, err := mapVersions(rawVersions)
	if err != nil {
		logger.ErrorContext(ctx, "ListProviderVersions found unrecognized version names", "error", err)
	}

	return vs, nil
}

func (a *ArtifactRegistryGeneric) GetProviderVersion(ctx context.Context, namespace string, name string, version string, os string, arch string) (*model.Provider, error) {
	req := &arpb.ListFilesRequest{
		Parent:   fmt.Sprintf("%s/repositories/%s", a.scope(), namespace),
		Filter:   fmt.Sprintf(`owner="%s/repositories/%s/packages/%s"`, a.scope(), namespace, name),
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

	providerBinName := ""
	shaSumName := ""
	shaSumSigName := ""
	gpgKeyName := ""
	namePrefix := fileNamePrefix(name, version)

	for _, f := range files {
		switch f.Name {
		case namePrefix + fmt.Sprintf("_%s_%s.zip", os, arch):
			providerBinName = f.Name
		case namePrefix + "_SHA256SUMS":
			shaSumName = f.Name
		case namePrefix + "_SHA256SUMS.sig":
			shaSumSigName = f.Name
		case namePrefix + "_gpg-public-key.pem":
			gpgKeyName = f.Name
		}
	}

	if providerBinName == "" {
		return nil, fmt.Errorf("provider binary not found for %s_%s_%s", version, os, arch)
	}
	if shaSumName == "" {
		return nil, fmt.Errorf("SHA256SUMS not found for %s_%s_%s", version, os, arch)
	}
	if shaSumSigName == "" {
		return nil, fmt.Errorf("SHA256SUMS.sig not found for %s_%s_%s", version, os, arch)
	}

	shaSums, err := a.parseSHASumFile(ctx, namespace, shaSumName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SHA256SUMS: %w", err)
	}

	keys, err := a.parseGPGKeys(ctx, namespace, gpgKeyName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GPG keys: %w", err)
	}

	downloadUrl := fmt.Sprintf("/download/provider/%s/asset/%s", namespace, providerBinName)
	SHASumURL := fmt.Sprintf("/download/provider/%s/asset/%s", namespace, shaSumName)
	SHASumSigURL := fmt.Sprintf("/download/provider/%s/asset/%s", namespace, shaSumSigName)

	p := &model.Provider{
		Protocols:           []string{"5.0"},
		OS:                  os,
		Arch:                arch,
		Filename:            providerBinName,
		DownloadURL:         downloadUrl,
		SHASumsURL:          SHASumURL,
		SHASumsSignatureURL: SHASumSigURL,
		SHASum:              shaSums[providerBinName],
		SigningKeys:         model.SigningKeys{GPGPublicKeys: keys},
	}

	return p, nil
}

func (a *ArtifactRegistryGeneric) GetProviderAsset(ctx context.Context, namespace string, fileName string) (io.ReadCloser, error) {
	u := fmt.Sprintf("%s/repositories/%s/files/%s:download", a.scope(), namespace, fileName)
	r, err := a.downloader.Download(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", fileName, err)
	}
	return r, nil
}

func (a *ArtifactRegistryGeneric) parseSHASumFile(ctx context.Context, namespace, fileName string) (map[string]string, error) {
	u := fmt.Sprintf("%s/repositories/%s/files/%s:download", a.scope(), namespace, fileName)
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
	u := fmt.Sprintf("%s/repositories/%s/files/%s:download", a.scope(), namespace, fileName)
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

func (a *ArtifactRegistryGeneric) scope() string {
	return fmt.Sprintf("projects/%s/locations/%s", a.cfg.ProjectID, a.cfg.Location)
}

func versionName(version, os, arch string) string {
	return fmt.Sprintf("%s_%s_%s", version, os, arch)
}

func fileNamePrefix(pkg, version string) string {
	return fmt.Sprintf("terraform-provider-%s_%s", pkg, version)
}

func mapVersions(rawVersions []string) (*model.ProviderVersions, error) {
	var merr error
	m := make(map[string][]model.Platform)
	for _, v := range rawVersions {
		version, os, arch, err := parseVersion(v)
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

func parseVersion(version string) (string, string, string, error) {
	parts := strings.Split(version, "_")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("invalid version format: %s", version)
	}
	return parts[0], parts[1], parts[2], nil
}
