# Didj Repair Tool

**Fix the LeapFrog Didj "Your Didj needs a tune up" error on Windows 10/11.**
No LeapFrog Connect, no Windows 7 virtual machine, no Flash, no Internet.

*[Instrucciones en español más abajo](#español)*

The LeapFrog Didj (2008) shows *"Your Didj needs a tune up"* when its system needs
to be reinstalled by LeapFrog Connect. LeapFrog Connect no longer works on modern
Windows and its servers are gone, so the old workaround needed a Windows 7 VM, a
portable Flash plug-in and manual editing of firmware packages.

This tool does the same job directly, in a step-by-step wizard (English / Español):

1. Reads the original LeapFrog firmware and system files straight from `DidjPlugin.exe`.
2. Detects and unlocks the Didj over USB, and gives it a drive letter.
3. Offers to delete leftover update folders (a common cause of the endless "tune up").
4. Checks and repairs the Didj file system (`chkdsk`).
5. Restores the system packages (Brio, bin, lib, fonts, audio, tutorials…).
6. Copies the firmware, verifies it, and safely ejects the Didj so it installs itself.

Tested on a real Didj that was stuck on the tune-up screen.

## What you need

| | |
|---|---|
| **`DidjRepairTool.exe`** | Download it from [Releases](../../releases). Use `DidjRepairTool_32bit.exe` on 32-bit Windows. |
| **`DidjPlugin.exe`** | LeapFrog's Didj plug-in, which contains the firmware. It is **not** included here (it is LeapFrog's copyrighted software). Put it in the same folder as the tool. No need to install it. |
| **USB cable** | It must carry **data**. Many cables only charge. |
| **Power** | Fresh batteries or the AC adapter. |

Other firmware sources also work: the `LeapFrogConnectOfflineSetup_Didj.exe` installer
(requires [7-Zip](https://www.7-zip.org/)), an existing LeapFrog Connect installation,
a `DIDJ-0x000E0003-*.lfp` package, or a folder with `kernel.bin` and `erootfs.jffs2`.

## How to use

1. Put `DidjRepairTool.exe` and `DidjPlugin.exe` in the same folder.
2. Run `DidjRepairTool.exe` and accept the administrator prompt.
   Windows SmartScreen may warn because the program is not signed:
   click **More info → Run anyway**.
3. Choose your language and **option 1 (Repair)**, then follow the steps.
   With the Didj **off**, connect the USB cable, then turn the Didj on.
4. **Do not turn off or unplug the Didj while it updates.** It restarts by itself when done.
5. Once it boots normally, run the tool again and use **option 3** to delete the
   update files left on the Didj, so it does not reinstall every time you connect it.

Other menu options: **2** shows battery, serial number and "needs repair" status.

> ⚠ Updating firmware carries a small risk of bricking the device, and the repair
> **erases the Didj data** (profiles and saved games). Use at your own risk.

## Troubleshooting

- **"Didj not found"** while the Didj says *USB connected*: the cable is almost
  certainly charge-only, or the port is bad. The tool prints a diagnostic report;
  if it lists no LeapFrog device (VID_0F63), Windows is receiving no data at all.
  Try another cable (one that transfers files from a phone) and a rear USB port.
- **Restoring takes long**: the Didj storage is slow; several thousand small files
  are copied. A progress counter is shown.
- To rename player profiles, use the **About Me** screen on the Didj itself.

## How it works

The Didj appears as a USB mass-storage device that stays locked until the host
sends LeapFrog vendor SCSI commands. The tool uses the same commands as LeapFrog
Connect, documented by the open-source
[OpenLFConnect](https://github.com/jrspruitt/OpenLFConnect) project:

| CDB | Meaning |
|---|---|
| `C2 00 …` | unlock (lets Windows mount the drive) |
| `C3 02 …` / `C3 03 …` / `C3 06 …` | read battery level / serial number / needs-repair flag |
| `C6 00 …` | disconnect; if update files are present, the Didj flashes them |

The firmware update is `kernel.bin` + `erootfs.jffs2` (with `.md5` files) copied to
`Base\firmware-LF_LF1000\` on the Didj drive. System packages from the `.lfp`
files (ZIP archives) go into `Base\`. `DidjPlugin.exe` is read with a built-in
CAB/MSZIP extractor, so no external tools are needed.

Background: [elinux.org – LeapFrog Pollux Platform: LFConnect](https://elinux.org/LeapFrog_Pollux_Platform:_LFConnect)
and the r/OpenLF guide *"How to fix 'Your Didj needs a tune up' error in 2021"*.

## Building from source

Requires [Go](https://go.dev/dl/) 1.21+. Cross-compiles from any OS:

```sh
./build.sh        # Linux / macOS
build.bat         # Windows
```

Output goes to `dist/`. GitHub Actions builds the executables on every push, and
attaches them to a release when you push a tag such as `v1.2`.

| File | Contents |
|---|---|
| `main.go` | Wizard and menu |
| `win.go` | Windows APIs: device detection, SCSI pass-through, volumes |
| `firmware.go` | Finding and reading firmware / `.lfp` packages |
| `cab.go` | Built-in CAB (MSZIP) extractor for `DidjPlugin.exe` |
| `i18n.go` | Spanish translations |

## License

GPL-3.0, see [LICENSE](LICENSE). Not affiliated with or endorsed by LeapFrog.
"LeapFrog" and "Didj" are trademarks of LeapFrog Enterprises, Inc.

---

## Español

**Arregla el error *"Your Didj needs a tune up"* de la LeapFrog Didj en Windows 10/11**,
sin LeapFrog Connect, sin Windows 7, sin Flash y sin Internet. El programa está en
inglés y en español (se elige al abrirlo).

**Necesitas:** `DidjRepairTool.exe` (en [Releases](../../releases)) y `DidjPlugin.exe`
de LeapFrog en la misma carpeta (no se incluye aquí por derechos de autor; no hace
falta instalarlo), un cable USB **de datos** y pilas nuevas.

**Uso:**

1. Ejecuta `DidjRepairTool.exe` y acepta los permisos de administrador
   (si sale SmartScreen: *Más información → Ejecutar de todas formas*).
2. Elige idioma y la **opción 1**. Con la Didj **apagada**, conéctala y enciéndela.
3. **No la apagues ni la desconectes** mientras se actualiza.
4. Cuando arranque bien, usa la **opción 3** para borrar los archivos de actualización.

⚠ La reparación **borra los datos de la Didj** (perfiles y partidas) y tiene un riesgo
pequeño de dejarla inservible. Úsalo bajo tu responsabilidad.

Si dice *"Didj not found"* aunque la consola muestre *USB connected*, casi seguro el
cable solo carga: prueba otro cable y otro puerto USB.
