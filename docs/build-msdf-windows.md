# Windows x64 DynamicMSDF build

Use Windows x64 and **64-bit Windows PowerShell 5.1 or PowerShell 7**. Install Git,
Go 1.27.3 or later, Visual Studio 2022/2026 with Desktop development with C++ and
a Windows SDK, and CMake >= 3.20 (the Visual Studio bundled CMake is detected).
For verified installer contents, put a full **7-Zip `7z.exe` or `7zz.exe`** on PATH.
MyGo uses installed NSIS or downloads its pinned NSIS into its user cache.
Network access to the public Go proxy/module checksum database and GitHub is needed.

From an empty directory:

```powershell
git clone --branch codex/dynamic-msdf-20261116 https://github.com/hey2495/cloudy.git
cd cloudy
powershell -ExecutionPolicy Bypass -File scripts/build-msdf-windows.ps1
# Or: pwsh -File scripts/build-msdf-windows.ps1
```

The default output is `build/MSDF-REPRO/windows-amd64/Cloudy.exe`, with
`cloudy_mtsdf.dll`, `licenses/`, `MSDF-SOURCE.json` and the NSIS setup EXE alongside.
Keep the portable directory together. Do not copy only the EXE.
`build/MSDF-REPRO/build-manifest.json` records the DLL/EXE hashes and installer
verification result; `exe-build-info.txt` records Go build metadata, and
`stage_dynamic_msdf_version.go.txt` preserves the generated SHA binding.
An existing output is refused to protect previous builds; use e.g.
`-OutDir build/MSDF-REPRO-2` for a subsequent build. `-CMake` accepts a CMake path.
Run this command from a Git checkout with these build scripts tracked.

The script reads the **remote, versioned replacement in `go.mod`** and downloads
it through `go mod download`. No neighboring MyGo checkout or private project path
is used. Currently this is public `github.com/hey2495/mygo`
`v0.7.1-0.20261116012629-0f79926a8a1d`, declaring the original
`github.com/egoist/mygo` module. This temporary contributor fork supports
[MyGo PR #1](https://github.com/nya-owo/mygo/pull/1); before upstream integration,
maintainers must switch to the accepted `nya-owo/mygo` version and revalidate.
The script accepts pinned remote replacements in either fork and rejects local paths.

Native MSDFGen/Skia sources come from that checksum-verified Go module. CMake
compiles the DLL from source with static MSVC runtime, then a Go probe checks its
actual SHA256, ABI1 and generator revision4, opens it through MyGo, and confirms
that incorrect SHA and missing DLL are rejected. The actual SHA is written as `app.stageDistanceDLLSHA` in the disposable
source copy. The generated source is saved beside the manifest; the compiled
EXE constant bytes and staged DLL are checked against that value. The ordinary
build retains the original SHA default. Runtime loading still uses only the DLL
beside the EXE and MyGo's SHA/ABI verification; missing or mismatched resources
continue to fall back to native font rendering. Toolchains/timestamps can change
DLL bytes, so rebuild and distribute the EXE and DLL as one pair.

All MyGo-generated configuration/resources live in a disposable tracked-source
copy. Tracked source and `mygo.json` in the checkout are not rewritten. Module
caches may be populated. No signing key, updater archive, upload, release, tag,
installation or application launch is performed. The temporary config omits
`updates` so this independent build does not enter the official update pipeline.

Licenses include the Cloudy and MyGo MIT licenses, MSDFGen's MIT license and
Skia's BSD license, copied verbatim from the application/module sources.
`licenses/dependency-lock.json` preserves native dependency provenance;
`MSDF-SOURCE.json` records the module version/checksum and DLL identity.

With 7-Zip on PATH, the script **extracts the generated NSIS installer without
running it** and compares EXE, DLL, source notice and all license hashes with
the staging directory. A missing/mismatched payload fails the build. Without
7-Zip (or without a generated installer), the manifest explicitly records
installer verification **HOLD**, even when the portable build succeeds. This
does not claim an installation has been tested.

This command targets **Windows amd64 only**. ARM64 keeps the unsupported-platform
MSDF stub and native text fallback; do not add this x64 DLL to ARM64 packages.
The existing Ubuntu cross-platform `.github/workflows/release.yml` is unchanged
and does **not** yet build/pair this DLL. Official releases still require a
maintainer to integrate a Windows x64 native build, notices, SHA binding and
installer checks alongside the existing ARM64/signing/update steps.
