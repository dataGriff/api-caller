# Install the latest apic release on Windows.
#   irm https://raw.githubusercontent.com/dataGriff/api-caller/main/install.ps1 | iex
#
# Options, as environment variables:
#   APIC_VERSION         a release to install, e.g. v1.2.3 (default: the latest)
#   APIC_INSTALL_DIR     where apic.exe goes (default: %LOCALAPPDATA%\Programs\apic)
#   APIC_NO_MODIFY_PATH  set to 1 to leave the user PATH alone
#   APIC_DOWNLOAD_BASE   a mirror of the releases, laid out as <base>/<tag>/<file>
#
# The zip is checked against the release's checksums.txt and the install is
# refused on a mismatch, as install.sh does. Works in Windows PowerShell 5.1
# and PowerShell 7.
#
# It runs in a script block so that `| iex` leaves no variables behind, and
# throws rather than exits, since exiting would close the caller's window.
& {
    $ErrorActionPreference = 'Stop'
    Set-StrictMode -Version 2
    $ProgressPreference = 'SilentlyContinue' # the progress bar makes downloads crawl in 5.1

    $repo = 'dataGriff/api-caller'
    $onWindows = [Environment]::OSVersion.Platform -eq 'Win32NT'

    # Windows PowerShell 5.1 does not offer TLS 1.2 by default.
    if ($PSVersionTable.PSVersion.Major -lt 6) {
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    }

    $arch = $null
    try {
        $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    } catch {
        # .NET Framework before 4.7.1; PROCESSOR_ARCHITEW6432 is set in a
        # 32-bit process on a 64-bit machine.
        $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    }
    switch -Regex ($arch) {
        '^(X64|AMD64)$' { $arch = 'amd64' }
        '^ARM64$' { $arch = 'arm64' }
        default { throw "unsupported architecture: $arch (apic is built for amd64 and arm64)" }
    }

    $version = $env:APIC_VERSION
    if (-not $version) {
        $latest = Invoke-RestMethod -UseBasicParsing -Uri "https://api.github.com/repos/$repo/releases/latest" -Headers @{ 'User-Agent' = 'apic-install' }
        $version = $latest.tag_name
        if (-not $version) { throw 'could not determine the latest version' }
    }
    if (-not $version.StartsWith('v')) { $version = "v$version" }
    $number = $version.Substring(1) # archives are named without the v

    $base = $env:APIC_DOWNLOAD_BASE
    if (-not $base) { $base = "https://github.com/$repo/releases/download" }
    $base = $base.TrimEnd('/')

    $dir = $env:APIC_INSTALL_DIR
    if (-not $dir) {
        if (-not $env:LOCALAPPDATA) { throw 'LOCALAPPDATA is not set; set APIC_INSTALL_DIR' }
        $dir = Join-Path $env:LOCALAPPDATA 'Programs\apic'
    }

    $name = "apic_${number}_windows_${arch}.zip"
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("apic-install-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        $zip = Join-Path $tmp $name
        $sums = Join-Path $tmp 'checksums.txt'
        Write-Host "downloading $base/$version/$name"
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$version/$name" -OutFile $zip
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$version/checksums.txt" -OutFile $sums

        $expected = $null
        foreach ($line in Get-Content -LiteralPath $sums) {
            $fields = $line -split '\s+'
            if ($fields.Count -ge 2 -and $fields[1] -eq $name) { $expected = $fields[0].ToLowerInvariant() }
        }
        if (-not $expected) { throw "checksum not found for $name" }
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $zip).Hash.ToLowerInvariant()
        if ($actual -ne $expected) { throw "checksum mismatch for $name (expected $expected, got $actual); not installing" }

        $unpacked = Join-Path $tmp 'unpacked'
        Expand-Archive -LiteralPath $zip -DestinationPath $unpacked -Force
        $exe = Join-Path $unpacked 'apic.exe'
        if (-not (Test-Path -LiteralPath $exe)) { throw "$name has no apic.exe" }
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        Copy-Item -LiteralPath $exe -Destination (Join-Path $dir 'apic.exe') -Force
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
    $installed = Join-Path $dir 'apic.exe'
    Write-Host "installed apic $version to $installed"

    $onPath = ($env:Path -split [IO.Path]::PathSeparator) -contains $dir
    if (-not $onPath) {
        if ($env:APIC_NO_MODIFY_PATH -eq '1' -or -not $onWindows) {
            Write-Host "add $dir to your PATH"
        } else {
            $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
            $entries = if ($userPath) { $userPath -split ';' } else { @() }
            if ($entries -notcontains $dir) {
                $updated = (@($entries | Where-Object { $_ }) + $dir) -join ';'
                [Environment]::SetEnvironmentVariable('Path', $updated, 'User')
                Write-Host "added $dir to your user PATH; open a new terminal to use it everywhere"
            }
            $env:Path = "$env:Path;$dir"
        }
    }
    & $installed version
}
