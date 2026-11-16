#requires -Version 5.1
[CmdletBinding()]
param([string]$OutDir = 'build/MSDF-REPRO', [string]$CMake = 'cmake')
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Run([string]$Tool, [string[]]$Arguments) {
    & $Tool @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Tool failed ($LASTEXITCODE)" }
}
function JsonCommand([string[]]$Arguments) {
    $json = & go @Arguments
    if ($LASTEXITCODE -ne 0) { throw "go $Arguments failed" }
    return ($json -join "`n" | ConvertFrom-Json)
}
function WriteUTF8([string]$Path, [string]$Value) {
    [IO.File]::WriteAllText($Path, $Value, (New-Object Text.UTF8Encoding($false)))
}

$root = Split-Path $PSScriptRoot -Parent
$savedLocation = Get-Location
$savedEnv = @{}
foreach ($key in @('CGO_ENABLED','GOOS','GOARCH','GOFLAGS','GOWORK','MYGO_LDFLAGS','MYGO_UPDATER_PRIVATE_KEY')) {
    $savedEnv[$key] = [Environment]::GetEnvironmentVariable($key, 'Process')
}
$work = $null
try {
    if ([Environment]::OSVersion.Platform -ne 'Win32NT' -or -not [Environment]::Is64BitProcess -or
        $env:PROCESSOR_ARCHITECTURE -ne 'AMD64') { throw 'Use 64-bit PowerShell on Windows x64.' }
    Set-Location $root
    foreach ($tool in @('go','git')) { Get-Command $tool -ErrorAction Stop | Out-Null }
    $vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio/Installer/vswhere.exe'
    if (!(Test-Path $vswhere)) { throw 'Install Visual Studio C++ tools and Windows SDK.' }
    $vs = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -format json | ConvertFrom-Json
    if (!$vs) { throw 'Visual Studio C++ x64 tools are required.' }
    $vs = @($vs)[0]
    if (!(Get-Command $CMake -ErrorAction SilentlyContinue)) {
        $CMake = Join-Path $vs.installationPath 'Common7/IDE/CommonExtensions/Microsoft/CMake/CMake/bin/cmake.exe'
    }
    if (!(Test-Path $CMake) -and !(Get-Command $CMake -ErrorAction SilentlyContinue)) { throw 'CMake >= 3.20 is required.' }
    # CMake verifies the SDK/compiler by compiling its compiler probe.
    $major = ([version]$vs.installationVersion).Major
    $generator = switch ($major) { 17 { 'Visual Studio 17 2022' }; 18 { 'Visual Studio 18 2026' }; default { throw 'Use Visual Studio 2022 or 2026.' } }
    $env:CGO_ENABLED='0'; $env:GOOS='windows'; $env:GOARCH='amd64'; $env:GOFLAGS='-mod=readonly'; $env:GOWORK='off'
    $env:MYGO_UPDATER_PRIVATE_KEY=$null; $env:MYGO_LDFLAGS=$null
    $module = JsonCommand @('list','-m','-json','github.com/egoist/mygo')
    $replacement = $module.Replace
    if (!$replacement -or !$replacement.Version -or $replacement.Path -notmatch '^github\.com/(hey2495|nya-owo)/mygo$') {
        throw 'MyGo must use a pinned remote hey2495/mygo or nya-owo/mygo replacement, never a local directory.'
    }
    $download = JsonCommand @('mod','download','-json',"$($replacement.Path)@$($replacement.Version)")
    $native = Join-Path $download.Dir 'native/mtsdf'
    if (!(Test-Path "$native/CMakeLists.txt")) { throw 'Pinned module lacks native MSDF source.' }
    $output = [IO.Path]::GetFullPath((Join-Path $root $OutDir))
    if (Test-Path $output) { throw "Output already exists; choose a new -OutDir: $output" }
    New-Item -ItemType Directory -Path $output | Out-Null
    $work = Join-Path ([IO.Path]::GetTempPath()) ('cloudy-msdf-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $work | Out-Null
    $source = Join-Path $work 'source'
    New-Item -ItemType Directory -Path $source | Out-Null
    # Copy only tracked source. MyGo may generate resources/config in this disposable copy.
    $files = & git -c core.quotepath=false ls-files
    if ($LASTEXITCODE -ne 0) { throw 'Run from a Git clone.' }
    foreach ($file in $files) {
        $dest = Join-Path $source $file
        New-Item -ItemType Directory -Force -Path (Split-Path $dest -Parent) | Out-Null
        Copy-Item -LiteralPath (Join-Path $root $file) -Destination $dest
    }
    $nativeBuild = Join-Path $work 'native'
    Run $CMake @('-S',$native,'-B',$nativeBuild,'-G',$generator,'-A','x64')
    Run $CMake @('--build',$nativeBuild,'--config','Release','--parallel')
    $dll = Join-Path $nativeBuild 'Release/cloudy_mtsdf.dll'
    $sha = (Get-FileHash -LiteralPath $dll -Algorithm SHA256).Hash
    Run 'go' @('run','./scripts/msdf-probe',$dll,$sha)
    $resources = Join-Path $source 'resources/windows-amd64'
    New-Item -ItemType Directory -Force -Path "$resources/licenses" | Out-Null
    Copy-Item -LiteralPath $dll -Destination "$resources/cloudy_mtsdf.dll"
    Copy-Item "$root/LICENSE" "$resources/licenses/CLOUDY-LICENSE"
    Copy-Item "$($download.Dir)/LICENSE" "$resources/licenses/MYGO-LICENSE"
    Copy-Item "$native/msdfgen/LICENSE.txt" "$resources/licenses/MSDFGEN-LICENSE.txt"
    Copy-Item "$native/skia-source/LICENSE" "$resources/licenses/SKIA-LICENSE"
    Copy-Item "$native/dependency-lock.json" "$resources/licenses/dependency-lock.json"
    $provenance = [ordered]@{ mygo=$replacement.Path; version=$replacement.Version; moduleSum=$download.Sum; dllSHA256=$sha; abi=1; revision=4; generator=$generator; go=(& go version) }
    WriteUTF8 "$resources/MSDF-SOURCE.json" ($provenance | ConvertTo-Json -Depth 8)
    # Disable signing/update generation only in the temporary delivery config.
    $config = Get-Content "$source/mygo.json" -Raw -Encoding UTF8 | ConvertFrom-Json
    $config.PSObject.Properties.Remove('updates')
    WriteUTF8 "$source/mygo.json" ($config | ConvertTo-Json -Depth 30)
    # Generate the binding only in the disposable source, leaving the clone untouched.
    $binding = "package app`n`n// Generated for this EXE/DLL pair; ABI1/revision4.`nconst stageDistanceDLLSHA = `"$sha`"`n"
    WriteUTF8 "$source/internal/app/stage_dynamic_msdf_version.go" $binding
    WriteUTF8 "$output/stage_dynamic_msdf_version.go.txt" $binding
    Set-Location $source
    Run 'go' @('tool','mygo','build','-platform','windows/amd64','-o',$output)
    $bundle = Join-Path $output 'windows-amd64'
    if ((Get-FileHash "$bundle/cloudy_mtsdf.dll").Hash -ne $sha) { throw 'Packaged DLL changed.' }
    $exe = Join-Path $bundle 'Cloudy.exe'
    $metadata = & go version -m $exe
    if ($LASTEXITCODE -ne 0) { throw 'Cannot read EXE build metadata.' }
    # trimpath omits linker flags from Go build metadata. Check the compiled
    # constant bytes instead; the exact generated source is saved for auditing.
    $exeBytes = [Text.Encoding]::ASCII.GetString([IO.File]::ReadAllBytes($exe))
    if (!$exeBytes.Contains($sha)) { throw 'EXE does not contain the generated SHA binding.' }
    WriteUTF8 "$output/exe-build-info.txt" ($metadata -join "`n")
    $provenance['exeSHA256'] = (Get-FileHash $exe).Hash
    # NSIS payload extraction is read-only; never run the installer.
    $installer = @(Get-ChildItem $bundle -Filter '*setup*.exe')
    $seven = Get-Command 7z -ErrorAction SilentlyContinue
    if (!$seven) { $seven = Get-Command 7zz -ErrorAction SilentlyContinue }
    $provenance['installerVerification'] = 'HOLD: install 7-Zip (7z/7zz on PATH) and rebuild to extract and verify the NSIS payload.'
    if ($installer.Count -eq 1 -and $seven) {
        $extract = Join-Path $work 'installer-payload'
        Run $seven.Source @('x','-y',"-o$extract",$installer[0].FullName)
        foreach ($name in @('Cloudy.exe','cloudy_mtsdf.dll','MSDF-SOURCE.json','licenses/CLOUDY-LICENSE','licenses/MYGO-LICENSE','licenses/MSDFGEN-LICENSE.txt','licenses/SKIA-LICENSE','licenses/dependency-lock.json')) {
            if ((Get-FileHash -LiteralPath (Join-Path $extract $name)).Hash -ne (Get-FileHash -LiteralPath (Join-Path $bundle $name)).Hash) { throw "Installer payload mismatch: $name" }
        }
        $provenance['installerVerification'] = 'PASS: extracted EXE/DLL/notices match staging SHA256'
        $provenance['installerSHA256'] = (Get-FileHash $installer[0].FullName).Hash
    }
    WriteUTF8 "$output/build-manifest.json" ($provenance | ConvertTo-Json -Depth 8)
    Write-Host "Built $bundle; DLL SHA256 $sha"
    Write-Host $provenance['installerVerification']
} finally {
    Set-Location $savedLocation
    foreach ($key in $savedEnv.Keys) { [Environment]::SetEnvironmentVariable($key,$savedEnv[$key],'Process') }
    if ($work -and (Test-Path $work)) {
        $resolved = [IO.Path]::GetFullPath($work)
        $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
        if (!$resolved.StartsWith($tempRoot,[StringComparison]::OrdinalIgnoreCase) -or (Split-Path $resolved -Leaf) -notmatch '^cloudy-msdf-[a-f0-9]{32}$') { throw 'Unexpected temporary directory; refusing cleanup.' }
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
