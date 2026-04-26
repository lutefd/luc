package extensions

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func PackPackage(path string) (string, PackageValidation, error) {
	validation, err := ValidatePackagePath(path)
	if err != nil {
		return "", PackageValidation{}, err
	}

	baseName := packageArchiveBaseName(validation.Manifest)
	outPath, err := filepath.Abs(baseName + ".tar.gz")
	if err != nil {
		return "", PackageValidation{}, err
	}

	file, err := os.Create(outPath)
	if err != nil {
		return "", PackageValidation{}, err
	}
	defer file.Close()

	gzw, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return "", PackageValidation{}, err
	}
	gzw.Name = ""
	gzw.Comment = ""
	gzw.ModTime = time.Unix(0, 0)

	tw := tar.NewWriter(gzw)
	rootPrefix := packageArchiveBaseName(validation.Manifest)

	files, err := collectPackageFiles(validation.Root)
	if err != nil {
		_ = tw.Close()
		_ = gzw.Close()
		return "", PackageValidation{}, err
	}
	for _, relPath := range files {
		srcPath := filepath.Join(validation.Root, relPath)
		info, err := os.Lstat(srcPath)
		if err != nil {
			_ = tw.Close()
			_ = gzw.Close()
			return "", PackageValidation{}, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			_ = tw.Close()
			_ = gzw.Close()
			return "", PackageValidation{}, fmt.Errorf("%s: symlinks are not supported in packages", srcPath)
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			_ = tw.Close()
			_ = gzw.Close()
			return "", PackageValidation{}, err
		}
		header.Name = filepath.ToSlash(filepath.Join(rootPrefix, relPath))
		header.ModTime = time.Unix(0, 0)
		header.AccessTime = time.Unix(0, 0)
		header.ChangeTime = time.Unix(0, 0)
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		if err := tw.WriteHeader(header); err != nil {
			_ = tw.Close()
			_ = gzw.Close()
			return "", PackageValidation{}, err
		}
		if info.IsDir() {
			continue
		}
		in, err := os.Open(srcPath)
		if err != nil {
			_ = tw.Close()
			_ = gzw.Close()
			return "", PackageValidation{}, err
		}
		if _, err := io.Copy(tw, in); err != nil {
			_ = in.Close()
			_ = tw.Close()
			_ = gzw.Close()
			return "", PackageValidation{}, err
		}
		if err := in.Close(); err != nil {
			_ = tw.Close()
			_ = gzw.Close()
			return "", PackageValidation{}, err
		}
	}

	if err := tw.Close(); err != nil {
		_ = gzw.Close()
		return "", PackageValidation{}, err
	}
	if err := gzw.Close(); err != nil {
		return "", PackageValidation{}, err
	}
	if err := file.Close(); err != nil {
		return "", PackageValidation{}, err
	}
	return outPath, validation, nil
}

func extractPackageArchive(path string) (string, func() error, error) {
	tempDir, err := os.MkdirTemp("", "luc-pkg-extract-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() error { return os.RemoveAll(tempDir) }

	file, err := os.Open(path)
	if err != nil {
		_ = cleanup()
		return "", nil, err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		_ = cleanup()
		return "", nil, err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = cleanup()
			return "", nil, err
		}
		if header == nil {
			continue
		}
		cleanName := filepath.Clean(header.Name)
		if cleanName == "." || cleanName == "/" || strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			_ = cleanup()
			return "", nil, fmt.Errorf("archive contains unsafe path %q", header.Name)
		}
		target := filepath.Join(tempDir, cleanName)
		if !strings.HasPrefix(target, tempDir+string(os.PathSeparator)) && target != tempDir {
			_ = cleanup()
			return "", nil, fmt.Errorf("archive path %q escapes extraction root", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				_ = cleanup()
				return "", nil, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				_ = cleanup()
				return "", nil, err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				_ = cleanup()
				return "", nil, err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				_ = cleanup()
				return "", nil, err
			}
			if err := out.Close(); err != nil {
				_ = cleanup()
				return "", nil, err
			}
		default:
			_ = cleanup()
			return "", nil, fmt.Errorf("archive entry %q uses unsupported type %c", header.Name, header.Typeflag)
		}
	}

	root, err := detectPackageRoot(tempDir)
	if err != nil {
		_ = cleanup()
		return "", nil, err
	}
	return root, cleanup, nil
}

func detectPackageRoot(root string) (string, error) {
	if _, err := os.Stat(filepath.Join(root, "luc.pkg.yaml")); err == nil {
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(root, entry.Name()))
		}
	}
	if len(dirs) != 1 {
		return "", fmt.Errorf("%s: archive must unpack to a single package root", root)
	}
	if _, err := os.Stat(filepath.Join(dirs[0], "luc.pkg.yaml")); err != nil {
		return "", fmt.Errorf("%s: archive root is missing luc.pkg.yaml", dirs[0])
	}
	return dirs[0], nil
}

func copyPackagePayload(srcRoot, dstRoot string) error {
	files, err := collectPackageFiles(srcRoot)
	if err != nil {
		return err
	}
	for _, relPath := range files {
		srcPath := filepath.Join(srcRoot, relPath)
		dstPath := filepath.Join(dstRoot, relPath)
		info, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlinks are not supported in packages", srcPath)
		}
		if info.IsDir() {
			if err := os.MkdirAll(dstPath, info.Mode().Perm()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		in, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = in.Close()
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = in.Close()
			_ = out.Close()
			return err
		}
		if err := in.Close(); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}

func collectPackageFiles(root string) ([]string, error) {
	var paths []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !isAllowedTopLevelPackageFile(name) {
			continue
		}
		if entry.IsDir() {
			if !isAllowedPackageCategory(name) && !isAllowedTopLevelPackageDir(name) {
				continue
			}
			err := filepath.WalkDir(filepath.Join(root, name), func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				rel = filepath.Clean(rel)
				if strings.HasPrefix(filepath.Base(path), ".git") {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				paths = append(paths, rel)
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}
		if isAllowedTopLevelPackageFile(name) {
			paths = append(paths, name)
		}
	}
	sort.Strings(paths)
	return paths, nil
}
