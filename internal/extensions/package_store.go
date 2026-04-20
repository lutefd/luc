package extensions

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type installedPackageDB struct {
	Packages []InstalledPackageRecord `json:"packages"`
}

func loadInstalledPackageRecords(storeRoot string) ([]InstalledPackageRecord, error) {
	path := filepath.Join(storeRoot, "installed.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var db installedPackageDB
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, err
	}
	for i := range db.Packages {
		db.Packages[i].Scope = PackageScope(strings.ToLower(strings.TrimSpace(string(db.Packages[i].Scope))))
	}
	sort.Slice(db.Packages, func(i, j int) bool {
		return strings.ToLower(db.Packages[i].Module) < strings.ToLower(db.Packages[j].Module)
	})
	return db.Packages, nil
}

func saveInstalledPackageRecords(storeRoot string, records []InstalledPackageRecord) error {
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		return err
	}
	sort.Slice(records, func(i, j int) bool {
		return strings.ToLower(records[i].Module) < strings.ToLower(records[j].Module)
	})
	db := installedPackageDB{Packages: records}
	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteFile(filepath.Join(storeRoot, "installed.json"), data, 0o644)
}

func findInstalledRecord(records []InstalledPackageRecord, module string) (InstalledPackageRecord, int) {
	for i, record := range records {
		if strings.EqualFold(strings.TrimSpace(record.Module), strings.TrimSpace(module)) {
			return record, i
		}
	}
	return InstalledPackageRecord{}, -1
}

func hydrateInstalledPackage(record InstalledPackageRecord) (InstalledPackage, error) {
	validation, err := validatePackageDir(record.PackageDir)
	if err != nil {
		return InstalledPackage{}, err
	}
	return InstalledPackage{
		Record:               record,
		Manifest:             validation.Manifest,
		Categories:           validation.Categories,
		ExecutableCategories: validation.ExecutableCategories,
	}, nil
}

func sortInstalledPackages(packages []InstalledPackage) {
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Record.Scope != packages[j].Record.Scope {
			return packages[i].Record.Scope < packages[j].Record.Scope
		}
		return strings.ToLower(packages[i].Record.Module) < strings.ToLower(packages[j].Record.Module)
	})
}
