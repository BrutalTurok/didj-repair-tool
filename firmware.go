package main

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Firmware holds the files the Didj needs to reinstall its system.
type Firmware struct {
	Source  string
	Kernel  []byte
	Erootfs []byte
	Extra   map[string][]byte // original meta.inf and .md5 files from the package
	SysPkgs []SysPkg          // system packages (Brio, bin, lib, Fonts...) that go in Base\
}

// SysPkg is a Didj system package; its top-level folder is copied into Base\.
type SysPkg struct {
	Name  string
	Top   string
	Files map[string][]byte // relative path (with /) → content
}

func parseMeta(b []byte) map[string]string {
	m := map[string]string{}
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
		if i := strings.Index(l, "="); i > 0 {
			m[strings.TrimSpace(l[:i])] = strings.Trim(strings.TrimSpace(l[i+1:]), `"`)
		}
	}
	return m
}

// parseSysPkg reads an .lfp and returns it if it is a system package that goes in Base\.
func parseSysPkg(data []byte) *SysPkg {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil
	}
	pkg := &SysPkg{Files: map[string][]byte{}}
	tops := map[string]bool{}
	for _, f := range zr.File {
		name := strings.TrimPrefix(strings.ReplaceAll(f.Name, `\`, "/"), "/")
		if strings.Contains(name, "..") {
			return nil
		}
		parts := strings.SplitN(name, "/", 2)
		tops[parts[0]] = true
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil
		}
		pkg.Files[name] = b
	}
	if len(tops) != 1 {
		return nil
	}
	for t := range tops {
		pkg.Top = t
	}
	meta, ok := pkg.Files[pkg.Top+"/meta.inf"]
	if !ok {
		return nil
	}
	m := parseMeta(meta)
	if !strings.EqualFold(m["Type"], "System") || strings.EqualFold(pkg.Top, "firmware-LF_LF1000") {
		return nil
	}
	pkg.Name = m["Name"]
	return pkg
}

// buildFromLFPs assembles the firmware and system packages from a set of .lfp files.
func buildFromLFPs(lfps map[string][]byte, src string) (*Firmware, error) {
	var fw *Firmware
	names := make([]string, 0, len(lfps))
	for n := range lfps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if strings.Contains(strings.ToLower(n), "0x000e0003") {
			if f, err := firmwareFromZipBytes(lfps[n], src); err == nil {
				fw = f
			}
		}
	}
	if fw == nil {
		for _, n := range names {
			if f, err := firmwareFromZipBytes(lfps[n], src); err == nil {
				fw = f
				break
			}
		}
	}
	if fw == nil {
		return nil, fmt.Errorf(L("Didj firmware not found in %s"), filepath.Base(src))
	}
	for _, n := range names {
		if p := parseSysPkg(lfps[n]); p != nil {
			fw.SysPkgs = append(fw.SysPkgs, *p)
		}
	}
	return fw, nil
}

// lfpsInDir reads every Didj .lfp file in a folder.
func lfpsInDir(dir string) map[string][]byte {
	res := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return res
	}
	for _, e := range entries {
		n := strings.ToLower(e.Name())
		if !e.IsDir() && strings.HasSuffix(n, ".lfp") && strings.HasPrefix(n, "didj") {
			if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil {
				res[e.Name()] = b
			}
		}
	}
	return res
}

func md5hex(b []byte) string {
	s := md5.Sum(b)
	return hex.EncodeToString(s[:])
}

func classify(name string) string {
	n := strings.ToLower(filepath.Base(strings.ReplaceAll(name, `\`, "/")))
	if strings.HasSuffix(n, ".md5") {
		return ""
	}
	if strings.Contains(n, "erootfs") && strings.HasSuffix(n, ".jffs2") {
		return "erootfs"
	}
	if strings.Contains(n, "kernel") && strings.HasSuffix(n, ".bin") {
		return "kernel"
	}
	return ""
}

// firmwareFromZipBytes lee un paquete .lfp (es un ZIP) y saca kernel y erootfs.
func firmwareFromZipBytes(data []byte, src string) (*Firmware, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	fw := &Firmware{Source: src, Extra: map[string][]byte{}}
	for _, f := range zr.File {
		kind := classify(f.Name)
		base := strings.ToLower(filepath.Base(strings.ReplaceAll(f.Name, `\`, "/")))
		if kind == "" && !f.FileInfo().IsDir() && (base == "meta.inf" || strings.HasSuffix(base, ".md5")) {
			if rc, err := f.Open(); err == nil {
				if b, err := io.ReadAll(rc); err == nil {
					fw.Extra[base] = b
				}
				rc.Close()
			}
			continue
		}
		if kind == "" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if kind == "kernel" {
			fw.Kernel = b
		} else {
			fw.Erootfs = b
		}
	}
	return fw, fw.validate()
}

func firmwareFromDir(dir string) (*Firmware, error) {
	fw := &Firmware{Source: dir}
	for _, sub := range []string{dir, filepath.Join(dir, "firmware-LF_LF1000")} {
		entries, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, e := range entries {
			kind := classify(e.Name())
			if kind == "" || e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(sub, e.Name()))
			if err != nil {
				continue
			}
			if kind == "kernel" && fw.Kernel == nil {
				fw.Kernel = b
			} else if kind == "erootfs" && fw.Erootfs == nil {
				fw.Erootfs = b
			}
		}
	}
	return fw, fw.validate()
}

func (fw *Firmware) validate() error {
	if fw.Kernel == nil || fw.Erootfs == nil {
		return fmt.Errorf("%s", L("it does not contain kernel.bin and erootfs.jffs2"))
	}
	if len(fw.Kernel) < 200*1024 || len(fw.Erootfs) < 1024*1024 {
		return fmt.Errorf("%s", L("the firmware files look too small (damaged?)"))
	}
	return nil
}

func loadFirmware(path string) (*Firmware, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return firmwareFromDir(path)
	}
	low := strings.ToLower(path)
	if strings.HasSuffix(low, ".lfp") || strings.HasSuffix(low, ".zip") {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		fw, err := firmwareFromZipBytes(data, path)
		if err == nil {
			addSiblingPkgs(fw, path)
			return fw, nil
		}
		// Not a plain ZIP: try 7-Zip.
		if z := find7z(); z != "" {
			return firmwareVia7z(z, path)
		}
		return nil, err
	}
	if strings.HasSuffix(low, ".exe") || strings.HasSuffix(low, ".cab") {
		// DidjPlugin.exe contains a CAB: open it without external tools.
		if data, err := os.ReadFile(path); err == nil {
			if lfps, err := cabFiles(data, isLFP); err == nil && len(lfps) > 0 {
				fmt.Println(L("   Extracting packages from"), filepath.Base(path), "...")
				if fw, err := buildFromLFPs(lfps, path); err == nil {
					return fw, nil
				}
			}
		}
	}
	if strings.HasSuffix(low, ".exe") || strings.HasSuffix(low, ".msi") {
		z := find7z()
		if z == "" {
			return nil, fmt.Errorf("%s", L("opening the installer needs 7-Zip (https://www.7-zip.org/). Alternatively, use DidjPlugin.exe or install LeapFrog Connect (no need to open it) and run this program again"))
		}
		return firmwareVia7z(z, path)
	}
	return nil, fmt.Errorf("%s", L("unrecognised file type"))
}

func find7z() string {
	cands := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "7-Zip", "7z.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "7-Zip", "7z.exe"),
		filepath.Join(os.Getenv("ProgramW6432"), "7-Zip", "7z.exe"),
		filepath.Join(exeDir(), "7z.exe"),
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath("7z.exe"); err == nil {
		return p
	}
	return ""
}

// firmwareVia7z unpacks (including nested archives) until it finds the Didj firmware.
func firmwareVia7z(sevenZip, archive string) (*Firmware, error) {
	tmp, err := os.MkdirTemp("", "didjfix-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	fmt.Println(L("   Extracting with 7-Zip (this may take a while)..."))

	queue := []string{archive}
	for depth := 0; depth < 4 && len(queue) > 0; depth++ {
		var next []string
		for i, a := range queue {
			out := filepath.Join(tmp, fmt.Sprintf("d%d_%d", depth, i))
			cmd := exec.Command(sevenZip, "x", "-y", "-o"+out, a)
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
			_ = cmd.Run()
			if fw := searchFirmwareIn(out, 12); fw != nil {
				return fw, nil
			}
			filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				ext := strings.ToLower(filepath.Ext(p))
				switch ext {
				case ".msi", ".cab", ".exe", ".zip", ".7z", ".rar", "":
					if info, e := d.Info(); e == nil && info.Size() > 512*1024 {
						next = append(next, p)
					}
				}
				return nil
			})
		}
		queue = next
	}
	return nil, fmt.Errorf(L("Didj firmware not found inside %s"), filepath.Base(archive))
}

// searchFirmwareIn looks for Didj .lfp packages or loose firmware files in a folder.
func searchFirmwareIn(root string, maxDepth int) *Firmware {
	var lfps []string
	var dirs []string
	base := strings.Count(filepath.Clean(root), string(os.PathSeparator))
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if strings.Count(filepath.Clean(p), string(os.PathSeparator))-base > maxDepth {
				return fs.SkipDir
			}
			return nil
		}
		n := strings.ToLower(d.Name())
		if strings.HasSuffix(n, ".lfp") && (strings.Contains(n, "didj") || strings.Contains(n, "0x000e0003")) {
			lfps = append(lfps, p)
		}
		if classify(n) == "erootfs" {
			dirs = append(dirs, filepath.Dir(p))
		}
		return nil
	})
	// Prefer the firmware package (0x000E0003) and the highest version.
	sort.Slice(lfps, func(i, j int) bool {
		ai := strings.Contains(strings.ToLower(lfps[i]), "0x000e0003")
		aj := strings.Contains(strings.ToLower(lfps[j]), "0x000e0003")
		if ai != aj {
			return ai
		}
		return strings.ToLower(filepath.Base(lfps[i])) > strings.ToLower(filepath.Base(lfps[j]))
	})
	for _, l := range lfps {
		if data, err := os.ReadFile(l); err == nil {
			if fw, err := firmwareFromZipBytes(data, l); err == nil {
				addSiblingPkgs(fw, l)
				return fw
			}
		}
	}
	for _, d := range dirs {
		if fw, err := firmwareFromDir(d); err == nil {
			return fw
		}
	}
	return nil
}

// autoFindFirmware checks the usual places: program folder, Downloads,
// Desktop and the LeapFrog Connect installation, if any.
func autoFindFirmware() *Firmware {
	home := os.Getenv("USERPROFILE")
	places := []struct {
		dir   string
		depth int
	}{
		{exeDir(), 3},
		{filepath.Join(os.Getenv("ProgramData"), "LeapFrog"), 8},
		{filepath.Join(os.Getenv("ProgramFiles"), "LeapFrog"), 8},
		{filepath.Join(os.Getenv("ProgramFiles(x86)"), "LeapFrog"), 8},
		{filepath.Join(home, "Downloads"), 2},
		{filepath.Join(home, "Desktop"), 2},
		{filepath.Join(home, "Documents", "LeapFrog"), 6},
	}
	for _, p := range places {
		if p.dir == "" {
			continue
		}
		if _, err := os.Stat(p.dir); err != nil {
			continue
		}
		if fw := searchFirmwareIn(p.dir, p.depth); fw != nil {
			return fw
		}
	}
	return nil
}

// findInstaller looks for DidjPlugin.exe or the LeapFrog Connect offline installer for Didj.
func findInstaller() string {
	home := os.Getenv("USERPROFILE")
	for _, dir := range []string{exeDir(), filepath.Join(home, "Downloads"), filepath.Join(home, "Desktop")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			n := strings.ToLower(e.Name())
			isLFC := strings.HasPrefix(n, "leapfrogconnect") && strings.Contains(n, "didj")
			isPlugin := strings.HasPrefix(n, "didjplugin")
			if (isLFC || isPlugin) && strings.HasSuffix(n, ".exe") {
				return filepath.Join(dir, e.Name())
			}
		}
	}
	return ""
}

func exeDir() string {
	p, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(p)
}

// addSiblingPkgs adds the system packages found next to the firmware .lfp.
func addSiblingPkgs(fw *Firmware, lfpPath string) {
	for _, data := range lfpsInDir(filepath.Dir(lfpPath)) {
		if p := parseSysPkg(data); p != nil {
			fw.SysPkgs = append(fw.SysPkgs, *p)
		}
	}
	sort.Slice(fw.SysPkgs, func(i, j int) bool { return fw.SysPkgs[i].Top < fw.SysPkgs[j].Top })
}
