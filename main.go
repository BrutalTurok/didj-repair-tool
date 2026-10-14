//go:build windows

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const version = "1.2"

var in = bufio.NewReader(os.Stdin)

func sleepSec(n int) { time.Sleep(time.Duration(n) * time.Second) }

func ask(prompt string) string {
	fmt.Print(prompt)
	s, _ := in.ReadString('\n')
	s = strings.TrimSpace(s)
	return strings.Trim(s, `"'`)
}

func yes(prompt string, def bool) bool {
	suf := L(" [Y/n]: ")
	if !def {
		suf = L(" [y/N]: ")
	}
	a := strings.ToLower(ask(prompt + suf))
	if a == "" {
		return def
	}
	return a == "s" || a == "si" || a == "sí" || a == "y"
}

func pause() { ask(L("\nPress Enter to continue...")) }

func title(s string) {
	fmt.Println()
	fmt.Println("──────────────────────────────────────────────────────────")
	fmt.Println(" " + s)
	fmt.Println("──────────────────────────────────────────────────────────")
}

func main() {
	setupConsole()
	if uiLangPrimary() == 0x0A { // Windows in Spanish → Spanish by default
		lang = "es"
	}
	if !isAdmin() {
		exe, _ := os.Executable()
		fmt.Println(L("This program needs administrator rights to talk to the Didj."))
		fmt.Println(L("Requesting permission..."))
		if relaunchAsAdmin(exe) {
			return
		}
		fmt.Println(L("Permission was not granted. Right-click the program → \"Run as administrator\"."))
		pause()
		return
	}

	chooseLanguage()
	for {
		fmt.Println()
		fmt.Println("╔════════════════════════════════════════════════════════╗")
		fmt.Printf(L("║   Didj Repair Tool %-4s — \"Your Didj needs a tune up\"  ║\n"), version)
		fmt.Println("╚════════════════════════════════════════════════════════╝")
		fmt.Println(L("  1) Repair the Didj (reinstall its system)"))
		fmt.Println(L("  2) Show Didj information (battery, status)"))
		fmt.Println(L("  3) Clean up update files (after repairing)"))
		fmt.Println(L("  4) Exit"))
		switch ask(L("\nChoose an option: ")) {
		case "1":
			repair()
		case "2":
			info()
		case "3":
			cleanup()
		case "4", "q", "s", "salir":
			return
		}
	}
}

func chooseLanguage() {
	def := "1"
	if lang == "es" {
		def = "2"
	}
	fmt.Println()
	fmt.Println("  Language / Idioma:")
	fmt.Println("    1) English")
	fmt.Println("    2) Español")
	switch ask(fmt.Sprintf("  [%s]: ", def)) {
	case "1":
		lang = "en"
	case "2":
		lang = "es"
	}
}

// ---------- Common steps ----------

func closeLeapFrogConnect() {
	// Find every process whose executable lives in a LeapFrog folder
	// (LeapFrog Connect and its Monitor.exe watcher, which opens it when the Didj is plugged in).
	ps := `Get-Process | Where-Object { $_.Path -like '*LeapFrog*' } | ForEach-Object { $_.Id }`
	out, _ := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Output()
	var pids []string
	for _, l := range strings.Fields(string(out)) {
		pids = append(pids, l)
	}
	if len(pids) > 0 {
		fmt.Println(L("\n   LeapFrog Connect (or its Monitor.exe watcher) is running and may"))
		fmt.Println(L("   interfere with the repair."))
		if yes(L("   Close it now?"), true) {
			for _, p := range pids {
				exec.Command("taskkill", "/F", "/PID", p).Run()
			}
			fmt.Println(L("   ✔ LeapFrog Connect closed."))
		}
	}
	// Monitor.exe auto-start (Run\Monitor registry value).
	for _, key := range []string{
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Run`,
		`HKLM\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`,
	} {
		o, err := exec.Command("reg", "query", key, "/v", "Monitor").Output()
		if err != nil || !strings.Contains(strings.ToLower(string(o)), "leapfrog") {
			continue
		}
		fmt.Println(L("\n   LeapFrog Connect is set to open automatically every time you plug in the Didj."))
		if yes(L("   Disable that auto-start? (reinstalling LeapFrog Connect turns it back on)"), true) {
			if exec.Command("reg", "delete", key, "/v", "Monitor", "/f").Run() == nil {
				fmt.Println(L("   ✔ Auto-start disabled."))
			}
		}
	}
}

// pendingUpdateDirs returns the update folders present on the Didj.
func pendingUpdateDirs(drive string) []string {
	var found []string
	for _, sub := range []string{"firmware-LF_LF1000", "bootstrap-LF_LF1000"} {
		p := filepath.Join(drive, "Base", sub)
		if _, err := os.Stat(p); err == nil {
			found = append(found, p)
		}
	}
	return found
}

func waitForDidj() *physDisk {
	for {
		fmt.Println()
		fmt.Println(L("  1. With the Didj turned OFF, connect it to the PC with the USB cable."))
		fmt.Println(L("  2. Turn the Didj on."))
		ask(L("  When done, press Enter..."))
		fmt.Print(L("  Looking for the Didj"))
		for i := 0; i < 30; i++ {
			if d := findDidj(); d != nil {
				fmt.Printf(L("\n   ✔ Didj found (%s %s, disk %d)\n"), d.Vendor, d.Product, d.Number)
				return d
			}
			fmt.Print(".")
			sleepSec(1)
		}
		fmt.Println(L("\n   ✘ Didj not found."))
		diagnose()
		fmt.Println(L("     • Try another USB cable (some only charge and carry no data) or another port."))
		fmt.Println(L("     • Turn the Didj off, unplug it and try again."))
		if !yes(L("Try again?"), true) {
			return nil
		}
	}
}

func battery(d *physDisk) int {
	b, err := didjSetting(d, 0x02, 1)
	if err != nil || len(b) == 0 {
		return 0
	}
	return int(b[0])
}

func needsRepair(d *physDisk) (bool, error) {
	b, err := didjSetting(d, 0x06, 1)
	if err != nil || len(b) == 0 {
		return false, err
	}
	return b[0] != 0, nil
}

func serial(d *physDisk) string {
	b, err := didjSetting(d, 0x03, 16)
	if err != nil || bytes.IndexByte(b, 0) >= 0 {
		return L("unknown")
	}
	return string(b)
}

var batteryNames = map[int]string{0: "unknown", 1: "CRITICAL", 2: "low", 3: "medium", 4: "high"} // translated with L()

func printInfo(d *physDisk) int {
	bat := battery(d)
	fmt.Printf(L("   Battery:            %s\n"), L(batteryNames[bat]))
	fmt.Printf(L("   Serial number:      %s\n"), serial(d))
	if nr, err := needsRepair(d); err == nil {
		if nr {
			fmt.Println(L("   Needs repair:       YES"))
		} else {
			fmt.Println(L("   Needs repair:       no"))
		}
	}
	return bat
}

// mountDidj unlocks the Didj and waits for Windows to give it a drive letter.
func mountDidj(d *physDisk) (string, error) {
	fmt.Println(L("\n   Unlocking the Didj so Windows sees it as a drive..."))
	if err := didjUnlock(d); err != nil {
		return "", err
	}
	fmt.Print(L("   Waiting for Windows to mount the drive"))
	for i := 0; i < 45; i++ {
		if d.Number < 0 {
			if p := probePath(d.Path); p != nil {
				d.Number = p.Number
			}
		}
		if vol, err := volumeOnDisk(d.Number); err == nil {
			letter := driveLetterOf(vol)
			if letter == "" {
				fmt.Println()
				fmt.Println(L("   The drive has no letter; assigning one automatically..."))
				l, err := assignDriveLetter(vol)
				if err != nil {
					return "", fmt.Errorf(L("could not assign a drive letter: %v"), err)
				}
				letter = l
				sleepSec(2)
			}
			if _, err := os.Stat(letter); err == nil {
				fmt.Printf(L("\n   ✔ Didj mounted as %s\n"), letter)
				return letter, nil
			}
		}
		fmt.Print(".")
		sleepSec(1)
	}
	return "", fmt.Errorf("%s", L("Windows did not mount the Didj drive"))
}

func ejectDidj(d *physDisk, drive string) error {
	if drive != "" {
		fmt.Println(L("   Saving all data to the Didj and unmounting the drive..."))
		if err := flushAndDismount(drive); err != nil {
			return err
		}
		sleepSec(1)
	}
	fmt.Println(L("   Sending the eject command to the Didj..."))
	return didjDisconnect(d)
}

func runChkdsk(drive string) {
	fmt.Println(L("\n   Running the Windows disk check (chkdsk)..."))
	cmd := exec.Command("chkdsk", strings.TrimSuffix(drive, `\`), "/F", "/X")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = cmd.Run()
}

// diagnose prints what Windows sees, to find out why the Didj is not detected.
func diagnose() {
	fmt.Println(L("\n   ── Diagnostics ──"))
	fmt.Println(L("   Storage drives Windows can see:"))
	devs := listStorageDevices()
	if len(devs) == 0 {
		fmt.Println(L("     (none)"))
	}
	for _, s := range devs {
		fmt.Println("     •", s)
	}
	fmt.Println(L("   LeapFrog USB devices (VID_0F63):"))
	ps := `Get-PnpDevice | Where-Object { $_.InstanceId -match 'VID_0F63|LEAPFROG|DIDJ' -or $_.FriendlyName -match 'LeapFrog|Didj' } | ` +
		`ForEach-Object { '{0} | {1} | {2} | {3} | {4}' -f $_.Status, $_.Class, $_.FriendlyName, $_.Present, $_.InstanceId }`
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Output()
	txt := strings.TrimSpace(string(out))
	if err != nil || txt == "" {
		fmt.Println(L("     (none)"))
	} else {
		for _, l := range strings.Split(txt, "\n") {
			fmt.Println("     •", strings.TrimSpace(l))
		}
	}
	fmt.Println(L("   (If you ask for help, copy this report: select it with the mouse and press Enter.)"))
}

// ---------- Option 1: Repair ----------

func getFirmware() *Firmware {
	title(L("Step 1 · Didj firmware"))
	fmt.Println(L("   Looking for the firmware on this PC..."))
	if fw := autoFindFirmware(); fw != nil {
		return fw
	}
	if inst := findInstaller(); inst != "" {
		fmt.Printf(L("   Found the installer: %s\n"), filepath.Base(inst))
		if fw, err := loadFirmware(inst); err == nil {
			return fw
		} else {
			fmt.Println("   ✘", err)
		}
	}
	fmt.Println()
	fmt.Println(L("   I could not find the firmware automatically. I need ONE of these:"))
	fmt.Println(L("     • \"DidjPlugin.exe\" (recommended) or the \"LeapFrogConnectOfflineSetup_Didj.exe\" installer (needs 7-Zip)"))
	fmt.Println(L("     • A \"DIDJ-0x000E0003-xxxxxx.lfp\" package"))
	fmt.Println(L("     • A folder with \"kernel.bin\" and \"erootfs.jffs2\""))
	fmt.Println(L("   Tip: drag the file or folder onto this window and press Enter."))
	for {
		p := ask(L("\n   Path (or Enter to go back to the menu): "))
		if p == "" {
			return nil
		}
		fw, err := loadFirmware(p)
		if err == nil {
			return fw
		}
		fmt.Println("   ✘", err)
	}
}

func repair() {
	title(L("Repair the Didj"))
	fmt.Println(L("   This wizard reinstalls the Didj system, just like LeapFrog Connect used to,"))
	fmt.Println(L("   but without needing Windows 7, Flash or LeapFrog's servers."))
	fmt.Println()
	fmt.Println(L("   ⚠ WARNING: updating the firmware carries a small risk of leaving the console"))
	fmt.Println(L("     unusable. Use FRESH batteries or the AC adapter, and do NOT unplug"))
	fmt.Println(L("     or turn off the Didj while it updates. You do this at your own risk."))
	fmt.Println(L("   ⚠ The Didj data WILL BE ERASED (profiles and saved games)."))
	if !yes(L("\n   Continue?"), true) {
		return
	}
	closeLeapFrogConnect()

	fw := getFirmware()
	if fw == nil {
		return
	}
	fmt.Printf(L("   ✔ Firmware ready (source: %s)\n"), fw.Source)
	fmt.Printf(L("     kernel.bin     %8d KB  md5 %s\n"), len(fw.Kernel)/1024, md5hex(fw.Kernel))
	fmt.Printf(L("     erootfs.jffs2  %8d KB  md5 %s\n"), len(fw.Erootfs)/1024, md5hex(fw.Erootfs))

	title(L("Step 2 · Connect the Didj"))
	d := waitForDidj()
	if d == nil {
		return
	}
	drive, err := mountDidj(d)
	if err != nil {
		fmt.Println("   ✘", err)
		pause()
		return
	}
	bat := printInfo(d)
	if bat == 1 || bat == 2 {
		fmt.Println(L("\n   ⚠ The battery is low. If it dies in the middle of the update the Didj may become unusable."))
		if a := strings.ToUpper(ask(L("   Type CONTINUE to go on anyway, or Enter to cancel: "))); a != "CONTINUAR" && a != "CONTINUE" {
			ejectDidj(d, drive)
			return
		}
	}

	// According to elinux.org, leftover update folders in Base\ make the Didj ask
	// for a "tune up" over and over. Deleting them is sometimes enough.
	if old := pendingUpdateDirs(drive); len(old) > 0 {
		title(L("Old update files found"))
		for _, p := range old {
			fmt.Println("   •", p)
		}
		fmt.Println(L("   These leftovers may be THE CAUSE of the \"tune up\" message. Deleting them does not"))
		fmt.Println(L("   touch the Didj system and is sometimes enough to fix it."))
		if yes(L("   Delete them and try first without reinstalling? (recommended)"), true) {
			for _, p := range old {
				if err := os.RemoveAll(p); err != nil {
					fmt.Println(L("   ✘ Could not delete"), p, ":", err)
				} else {
					fmt.Println(L("   ✔ Deleted"), p)
				}
			}
			if err := ejectDidj(d, drive); err != nil {
				fmt.Println("   ✘", err)
			}
			fmt.Println(L("\n   Unplug the Didj, turn it off and on again."))
			fmt.Println(L("   If \"tune up\" still appears, run option 1 again to reinstall."))
			pause()
			return
		}
		// When reinstalling, remove any old bootloader (bootstrap) so it is not flashed by mistake.
		os.RemoveAll(filepath.Join(drive, "Base", "bootstrap-LF_LF1000"))
	}

	title(L("Step 3 · Check the Didj disk"))
	fmt.Println(L("   If the Didj file system is damaged, Windows can repair it."))
	if yes(L("   Check and repair the Didj disk now? (recommended)"), true) {
		runChkdsk(drive)
		// chkdsk dismounts the drive; wait for it to come back.
		if _, err := os.Stat(drive); err != nil {
			sleepSec(3)
		}
	}

	if len(fw.SysPkgs) > 0 {
		title(L("Step 4 · Restore the system files"))
		fmt.Printf(L("   Found %d original LeapFrog system packages\n"), len(fw.SysPkgs))
		fmt.Println(L("   (Brio, bin, lib, Fonts, audio, tutorials...). If any of them is damaged on the"))
		fmt.Println(L("   Didj, it may be causing the \"tune up\". Restoring them only copies files."))
		if yes(L("   Restore them into the Didj Base folder? (recommended)"), true) {
			if err := restoreSysPkgs(drive, fw.SysPkgs); err != nil {
				fmt.Println(L("   ✘ Error restoring:"), err)
				if !yes(L("   Continue with the firmware anyway?"), false) {
					ejectDidj(d, drive)
					return
				}
			}
		}
	}

	title(L("Step 5 · Copy the firmware to the Didj"))
	if err := copyFirmware(drive, fw); err != nil {
		fmt.Println(L("   ✘ Error copying the firmware:"), err)
		fmt.Println(L("     The Didj system was not touched. Unplug it and try again."))
		pause()
		return
	}

	title(L("Step 6 · Install"))
	fmt.Println(L("   When you press Enter the Didj will be ejected and start installing the new system."))
	fmt.Println(L("   ⚠ From then on do NOT turn it off or unplug it until it finishes"))
	fmt.Println(L("     and restarts by itself (it may take several minutes)."))
	ask(L("\n   Press Enter to start the installation..."))
	if err := ejectDidj(d, drive); err != nil {
		fmt.Println(L("   ✘ Error ejecting:"), err)
		fmt.Println(L("     The files are already on the Didj. Try ejecting it with \"Safely Remove Hardware\"."))
		pause()
		return
	}
	fmt.Println()
	fmt.Println(L("   ✔ Done. The Didj should now show that it is updating."))
	fmt.Println(L("     Wait for it to finish and restart. Then you can unplug it."))
	fmt.Println()
	fmt.Println(L("   Once it boots fine, use menu option 3 to delete the update files"))
	fmt.Println(L("   left on the Didj (so it does not reinstall every time you connect it)."))
	pause()
}

func copyFirmware(drive string, fw *Firmware) error {
	base := filepath.Join(drive, "Base")
	if _, err := os.Stat(base); err != nil {
		fmt.Println(L("   (The Base folder does not exist on the Didj; it will be created.)"))
		if err := os.MkdirAll(base, 0755); err != nil {
			return err
		}
	}
	dir := filepath.Join(base, "firmware-LF_LF1000")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Use LeapFrog's original .md5 and meta.inf files when they match;
	// otherwise generate them (format: hex digest + newline).
	md5For := func(name string, data []byte) []byte {
		sum := md5hex(data)
		if b, ok := fw.Extra[name]; ok && strings.HasPrefix(strings.TrimSpace(string(b)), sum) {
			return b
		}
		return []byte(sum + "\n")
	}
	files := []struct {
		name string
		data []byte
	}{
		{"kernel.bin", fw.Kernel},
		{"kernel.md5", md5For("kernel.md5", fw.Kernel)},
		{"erootfs.jffs2", fw.Erootfs},
		{"erootfs.md5", md5For("erootfs.md5", fw.Erootfs)},
	}
	for _, extra := range []string{"meta.inf", "packagefiles.md5"} {
		if b, ok := fw.Extra[extra]; ok {
			files = append(files, struct {
				name string
				data []byte
			}{extra, b})
		}
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		fmt.Printf(L("   Copying %-14s "), f.name)
		if err := writeSync(p, f.data); err != nil {
			fmt.Println("✘")
			return err
		}
		back, err := os.ReadFile(p)
		if err != nil || md5hex(back) != md5hex(f.data) {
			fmt.Println("✘")
			return fmt.Errorf(L("the copy of %s does not match the original"), f.name)
		}
		fmt.Println("✔")
	}
	return nil
}

func restoreSysPkgs(drive string, pkgs []SysPkg) error {
	base := filepath.Join(drive, "Base")
	if err := os.MkdirAll(base, 0755); err != nil {
		return err
	}
	for _, p := range pkgs {
		total := len(p.Files)
		done := 0
		madeDirs := map[string]bool{}
		for rel, data := range p.Files {
			dst := filepath.Join(base, filepath.FromSlash(rel))
			if dir := filepath.Dir(dst); !madeDirs[dir] {
				if err := os.MkdirAll(dir, 0755); err != nil {
					fmt.Println(" ✘")
					return err
				}
				madeDirs[dir] = true
			}
			// No per-file flush (very slow on the Didj): Windows flushes
			// everything at once when the Didj is ejected.
			if err := os.WriteFile(dst, data, 0644); err != nil {
				fmt.Println(" ✘")
				return fmt.Errorf("%s: %v", rel, err)
			}
			done++
			fmt.Printf(L("\r   Restoring %-16s %4d/%-4d files"), p.Top, done, total)
		}
		fmt.Println(" ✔")
	}
	return nil
}

func writeSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ---------- Option 2: Information ----------

func info() {
	title(L("Didj information"))
	closeLeapFrogConnect()
	d := waitForDidj()
	if d == nil {
		return
	}
	drive, err := mountDidj(d)
	if err != nil {
		fmt.Println("   ✘", err)
	}
	fmt.Println()
	printInfo(d)
	if drive != "" {
		if _, err := os.Stat(filepath.Join(drive, "Base", "firmware-LF_LF1000")); err == nil {
			fmt.Println(L("   There are pending update files on the Didj (Base\\firmware-LF_LF1000)."))
			fmt.Println(L("   ⚠ When ejected, the Didj will install them. If it is already repaired, use option 3 first."))
		}
	}
	ask(L("\n   Press Enter to eject the Didj..."))
	if err := ejectDidj(d, drive); err != nil {
		fmt.Println("   ✘", err)
	} else {
		fmt.Println(L("   ✔ You can now unplug the Didj."))
	}
}

// ---------- Option 3: Clean up ----------

func cleanup() {
	title(L("Clean up update files"))
	fmt.Println(L("   Use this option ONLY when the Didj has finished updating and"))
	fmt.Println(L("   boots normally."))
	if !yes(L("   Continue?"), true) {
		return
	}
	closeLeapFrogConnect()
	d := waitForDidj()
	if d == nil {
		return
	}
	drive, err := mountDidj(d)
	if err != nil {
		fmt.Println("   ✘", err)
		pause()
		return
	}
	printInfo(d)
	removed := false
	for _, sub := range []string{"firmware-LF_LF1000", "bootstrap-LF_LF1000"} {
		p := filepath.Join(drive, "Base", sub)
		if _, err := os.Stat(p); err == nil {
			if err := os.RemoveAll(p); err != nil {
				fmt.Println(L("   ✘ Could not delete"), p, ":", err)
			} else {
				fmt.Println(L("   ✔ Deleted"), p)
				removed = true
			}
		}
	}
	if !removed {
		fmt.Println(L("   There were no update files on the Didj."))
	}
	if err := ejectDidj(d, drive); err != nil {
		fmt.Println("   ✘", err)
	} else {
		fmt.Println(L("   ✔ Didj ejected. You can now unplug it."))
	}
	pause()
}
