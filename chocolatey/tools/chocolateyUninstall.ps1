$ErrorActionPreference = 'Stop'

# Install-ChocolateyZipPackage auto-shims every .exe it unzips. Chocolatey removes
# those shims automatically on uninstall, and the unzipped tools dir is purged with
# the package — so no manual file removal is needed here. This script exists so the
# package has an explicit, auditable uninstall entry point; extend it if the install
# script ever creates state outside the tools directory.

$packageName = 'derpy'

# Remove the media-controls Start Menu shortcut created by chocolateyInstall.ps1
# (it lives outside the tools dir, so Chocolatey won't purge it automatically).
foreach ($base in @($env:ProgramData, $env:APPDATA)) {
    $lnk = Join-Path $base 'Microsoft\Windows\Start Menu\Programs\Derpy.lnk'
    if (Test-Path $lnk) {
        Remove-Item $lnk -Force -ErrorAction SilentlyContinue
    }
}

Write-Host "$packageName uninstalled. Shims and unpacked files are removed by Chocolatey."
