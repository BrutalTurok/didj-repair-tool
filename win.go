//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procFindFirstVolumeW                 = kernel32.NewProc("FindFirstVolumeW")
	procFindNextVolumeW                  = kernel32.NewProc("FindNextVolumeW")
	procFindVolumeClose                  = kernel32.NewProc("FindVolumeClose")
	procGetVolumePathNamesForVolumeNameW = kernel32.NewProc("GetVolumePathNamesForVolumeNameW")
	procSetVolumeMountPointW             = kernel32.NewProc("SetVolumeMountPointW")
	procGetLogicalDrives                 = kernel32.NewProc("GetLogicalDrives")
	procSetConsoleOutputCP               = kernel32.NewProc("SetConsoleOutputCP")
	procSetConsoleCP                     = kernel32.NewProc("SetConsoleCP")
	procIsUserAnAdmin                    = shell32.NewProc("IsUserAnAdmin")
	procGetUserDefaultUILanguage         = kernel32.NewProc("GetUserDefaultUILanguage")
	procShellExecuteW                    = shell32.NewProc("ShellExecuteW")
)

const (
	ioctlStorageQueryProperty       = 0x002D1400
	ioctlScsiPassThrough            = 0x0004D004
	ioctlVolumeGetVolumeDiskExtents = 0x00560000
	fsctlLockVolume                 = 0x00090018
	fsctlDismountVolume             = 0x00090020

	scsiDataOut         = 0
	scsiDataIn          = 1
	scsiDataUnspecified = 2
)

func setupConsole() {
	procSetConsoleOutputCP.Call(65001)
	procSetConsoleCP.Call(65001)
}

// uiLangPrimary returns the primary Windows UI language (0x0A = Spanish).
func uiLangPrimary() int {
	r, _, _ := procGetUserDefaultUILanguage.Call()
	return int(r) & 0x3FF
}

func isAdmin() bool {
	r, _, _ := procIsUserAnAdmin.Call()
	return r != 0
}

// relaunchAsAdmin restarts the program asking for administrator rights.
func relaunchAsAdmin(exe string) bool {
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, 1)
	return r > 32
}

func openDevice(path string, access uint32) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	return syscall.CreateFile(p, access,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil,
		syscall.OPEN_EXISTING, 0, 0)
}

func ioctl(h syscall.Handle, code uint32, in []byte, out []byte) (uint32, error) {
	var ret uint32
	var inPtr, outPtr *byte
	if len(in) > 0 {
		inPtr = &in[0]
	}
	if len(out) > 0 {
		outPtr = &out[0]
	}
	err := syscall.DeviceIoControl(h, code, inPtr, uint32(len(in)), outPtr, uint32(len(out)), &ret, nil)
	return ret, err
}

// ---------- Didj detection ----------

type physDisk struct {
	Number  int
	Path    string
	Vendor  string
	Product string
}

func cString(buf []byte, off uint32) string {
	if off == 0 || int(off) >= len(buf) {
		return ""
	}
	end := int(off)
	for end < len(buf) && buf[end] != 0 {
		end++
	}
	return strings.TrimSpace(string(buf[off:end]))
}

func queryDisk(n int) (*physDisk, error) {
	path := fmt.Sprintf(`\\.\PhysicalDrive%d`, n)
	h, err := openDevice(path, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(h)
	q := make([]byte, 12) // StorageDeviceProperty, PropertyStandardQuery
	out := make([]byte, 1024)
	if _, err := ioctl(h, ioctlStorageQueryProperty, q, out); err != nil {
		return nil, err
	}
	le := binary.LittleEndian
	return &physDisk{
		Number:  n,
		Path:    path,
		Vendor:  cString(out, le.Uint32(out[12:])),
		Product: cString(out, le.Uint32(out[16:])),
	}, nil
}

func isDidjName(s string) bool {
	v := strings.ToLower(s)
	return strings.Contains(v, "leapfrog") || strings.Contains(v, "didj") || strings.Contains(v, "ven_lf") || strings.Contains(v, "lf1000")
}

func findDidj() *physDisk {
	// 1) Windows device interfaces (works even while the drive reports no media).
	for _, g := range []syscall.GUID{guidDevinterfaceDisk, guidDevinterfaceCdrom} {
		for _, p := range interfacePaths(g) {
			if d := probePath(p); d != nil && (isDidjName(p) || isDidjName(d.Vendor+" "+d.Product)) {
				return d
			}
		}
	}
	// 2) Classic method: \\.\PhysicalDriveN
	for i := 0; i < 64; i++ {
		d, err := queryDisk(i)
		if err != nil || d == nil {
			continue
		}
		if isDidjName(d.Vendor + " " + d.Product) {
			return d
		}
	}
	return nil
}

// probePath opens an interface path and reads vendor, product and disk number.
func probePath(path string) *physDisk {
	h, err := openDevice(path, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(h)
	d := &physDisk{Number: -1, Path: path}
	q := make([]byte, 12)
	out := make([]byte, 1024)
	if _, err := ioctl(h, ioctlStorageQueryProperty, q, out); err == nil {
		le := binary.LittleEndian
		d.Vendor = cString(out, le.Uint32(out[12:]))
		d.Product = cString(out, le.Uint32(out[16:]))
	}
	num := make([]byte, 12)
	if _, err := ioctl(h, ioctlStorageGetDeviceNumber, nil, num); err == nil {
		d.Number = int(binary.LittleEndian.Uint32(num[4:]))
	}
	return d
}

var (
	setupapi                             = syscall.NewLazyDLL("setupapi.dll")
	procSetupDiGetClassDevsW             = setupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces      = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetailW = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList     = setupapi.NewProc("SetupDiDestroyDeviceInfoList")

	guidDevinterfaceDisk  = syscall.GUID{Data1: 0x53f56307, Data2: 0xb6bf, Data3: 0x11d0, Data4: [8]byte{0x94, 0xf2, 0x00, 0xa0, 0xc9, 0x1e, 0xfb, 0x8b}}
	guidDevinterfaceCdrom = syscall.GUID{Data1: 0x53f56308, Data2: 0xb6bf, Data3: 0x11d0, Data4: [8]byte{0x94, 0xf2, 0x00, 0xa0, 0xc9, 0x1e, 0xfb, 0x8b}}
)

const ioctlStorageGetDeviceNumber = 0x002D1080

func interfacePaths(g syscall.GUID) []string {
	const digcfPresent, digcfDeviceInterface = 0x2, 0x10
	hset, _, _ := procSetupDiGetClassDevsW.Call(uintptr(unsafe.Pointer(&g)), 0, 0, digcfPresent|digcfDeviceInterface)
	if syscall.Handle(hset) == syscall.InvalidHandle || hset == 0 {
		return nil
	}
	defer procSetupDiDestroyDeviceInfoList.Call(hset)
	ptr := int(unsafe.Sizeof(uintptr(0)))
	ifSize := 28
	detailCb := 6
	if ptr == 8 {
		ifSize, detailCb = 32, 8
	}
	var paths []string
	for i := 0; i < 256; i++ {
		ifData := make([]byte, ifSize)
		binary.LittleEndian.PutUint32(ifData, uint32(ifSize))
		r, _, _ := procSetupDiEnumDeviceInterfaces.Call(hset, 0, uintptr(unsafe.Pointer(&g)), uintptr(i), uintptr(unsafe.Pointer(&ifData[0])))
		if r == 0 {
			break
		}
		detail := make([]byte, 4096)
		binary.LittleEndian.PutUint32(detail, uint32(detailCb))
		var need uint32
		r, _, _ = procSetupDiGetDeviceInterfaceDetailW.Call(hset, uintptr(unsafe.Pointer(&ifData[0])),
			uintptr(unsafe.Pointer(&detail[0])), uintptr(len(detail)), uintptr(unsafe.Pointer(&need)), 0)
		if r == 0 {
			continue
		}
		u := make([]uint16, (len(detail)-4)/2)
		for j := range u {
			u[j] = binary.LittleEndian.Uint16(detail[4+2*j:])
		}
		paths = append(paths, syscall.UTF16ToString(u))
	}
	return paths
}

// listStorageDevices returns every storage device seen (for diagnostics).
func listStorageDevices() []string {
	var out []string
	for _, g := range []syscall.GUID{guidDevinterfaceDisk, guidDevinterfaceCdrom} {
		for _, p := range interfacePaths(g) {
			s := p
			if d := probePath(p); d != nil {
				s = fmt.Sprintf("disco %d: %s %s  [%s]", d.Number, d.Vendor, d.Product, p)
			}
			out = append(out, s)
		}
	}
	return out
}

// ---------- LeapFrog vendor SCSI commands ----------
// The same ones LeapFrog Connect uses (documented by OpenLFConnect):
//   C1 lock, C2 unlock (mount), C3 get_setting, C6 disconnect (eject / start update)

func scsiCommand(disk *physDisk, cdb []byte, readLen int) ([]byte, error) {
	h, err := openDevice(disk.Path, syscall.GENERIC_READ|syscall.GENERIC_WRITE)
	if err != nil {
		return nil, fmt.Errorf(L("could not open %s: %v"), disk.Path, err)
	}
	defer syscall.CloseHandle(h)

	ptr := int(unsafe.Sizeof(uintptr(0)))
	var offDataBuf, offSenseOff, offCdb, structSize int
	if ptr == 8 {
		offDataBuf, offSenseOff, offCdb, structSize = 24, 32, 36, 56
	} else {
		offDataBuf, offSenseOff, offCdb, structSize = 20, 24, 28, 44
	}
	const senseLen = 32
	senseOff := structSize
	dataOff := structSize + senseLen
	dataLen := readLen
	if dataLen < 0 {
		dataLen = 0
	}
	buf := make([]byte, dataOff+dataLen+16)
	le := binary.LittleEndian
	le.PutUint16(buf[0:], uint16(structSize)) // Length
	buf[6] = byte(len(cdb))                   // CdbLength
	buf[7] = senseLen                         // SenseInfoLength
	if readLen > 0 {
		buf[8] = scsiDataIn
	} else {
		buf[8] = scsiDataUnspecified
	}
	le.PutUint32(buf[12:], uint32(dataLen)) // DataTransferLength
	le.PutUint32(buf[16:], 20)              // TimeOutValue (s)
	if ptr == 8 {
		le.PutUint64(buf[offDataBuf:], uint64(dataOff))
	} else {
		le.PutUint32(buf[offDataBuf:], uint32(dataOff))
	}
	le.PutUint32(buf[offSenseOff:], uint32(senseOff))
	copy(buf[offCdb:offCdb+16], cdb)

	if _, err := ioctl(h, ioctlScsiPassThrough, buf, buf); err != nil {
		return nil, fmt.Errorf(L("SCSI error: %v"), err)
	}
	if status := buf[1]; status != 0 {
		return nil, fmt.Errorf(L("the Didj replied with SCSI status 0x%02X"), status)
	}
	return buf[dataOff : dataOff+dataLen], nil
}

func didjCDB(cmd, arg byte) []byte {
	cdb := make([]byte, 10)
	cdb[0] = cmd
	cdb[1] = arg
	return cdb
}

func didjUnlock(d *physDisk) error {
	_, err := scsiCommand(d, didjCDB(0xC2, 0x00), 0)
	return err
}

func didjDisconnect(d *physDisk) error {
	_, err := scsiCommand(d, didjCDB(0xC6, 0x00), 0)
	return err
}

func didjSetting(d *physDisk, arg byte, n int) ([]byte, error) {
	return scsiCommand(d, didjCDB(0xC3, arg), n)
}

// ---------- Volumes and drive letters ----------

func utf16ToString(b []uint16) string { return syscall.UTF16ToString(b) }

// volumeOnDisk returns the GUID name of the volume on the given disk.
func volumeOnDisk(diskNum int) (string, error) {
	buf := make([]uint16, 260)
	h, _, err := procFindFirstVolumeW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if syscall.Handle(h) == syscall.InvalidHandle {
		return "", err
	}
	defer procFindVolumeClose.Call(h)
	for {
		name := utf16ToString(buf)
		if diskOfVolume(name) == diskNum {
			return name, nil
		}
		r, _, _ := procFindNextVolumeW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if r == 0 {
			break
		}
	}
	return "", fmt.Errorf("%s", L("volume not found"))
}

func diskOfVolume(volName string) int {
	p := strings.TrimSuffix(volName, `\`)
	h, err := openDevice(p, 0)
	if err != nil {
		return -1
	}
	defer syscall.CloseHandle(h)
	out := make([]byte, 256)
	if _, err := ioctl(h, ioctlVolumeGetVolumeDiskExtents, nil, out); err != nil {
		return -1
	}
	if binary.LittleEndian.Uint32(out[0:]) < 1 {
		return -1
	}
	return int(binary.LittleEndian.Uint32(out[8:]))
}

func driveLetterOf(volName string) string {
	buf := make([]uint16, 1024)
	var ret uint32
	v, _ := syscall.UTF16PtrFromString(volName)
	r, _, _ := procGetVolumePathNamesForVolumeNameW.Call(uintptr(unsafe.Pointer(v)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&ret)))
	if r == 0 {
		return ""
	}
	// NUL-separated list of strings
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] == 0 {
			if i == start {
				break
			}
			s := syscall.UTF16ToString(buf[start:i])
			if len(s) == 3 && s[1] == ':' {
				return s
			}
			start = i + 1
		}
	}
	return ""
}

func assignDriveLetter(volName string) (string, error) {
	mask, _, _ := procGetLogicalDrives.Call()
	for l := 'Z'; l >= 'D'; l-- {
		if mask&(1<<uint(l-'A')) == 0 {
			mp := string(l) + `:\`
			a, _ := syscall.UTF16PtrFromString(mp)
			b, _ := syscall.UTF16PtrFromString(volName)
			r, _, err := procSetVolumeMountPointW.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)))
			if r != 0 {
				return mp, nil
			}
			return "", err
		}
	}
	return "", fmt.Errorf("%s", L("no free drive letters"))
}

// flushAndDismount flushes the Windows cache and dismounts the volume (like "Safely Remove").
func flushAndDismount(drive string) error {
	p := `\\.\` + strings.TrimSuffix(drive, `\`)
	h, err := openDevice(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	if err := syscall.FlushFileBuffers(h); err != nil {
		return fmt.Errorf(L("could not flush the cache: %v"), err)
	}
	locked := false
	for i := 0; i < 10; i++ {
		if _, err := ioctl(h, fsctlLockVolume, nil, nil); err == nil {
			locked = true
			break
		}
		sleepSec(1)
	}
	if !locked {
		fmt.Println(L("   (Note: could not lock the drive; is an Explorer window open on it? Unmounting anyway.)"))
	}
	if _, err := ioctl(h, fsctlDismountVolume, nil, nil); err != nil {
		return fmt.Errorf(L("could not unmount: %v"), err)
	}
	return nil
}
