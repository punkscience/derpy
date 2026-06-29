$ErrorActionPreference = 'Stop'

$packageName    = 'derpy'
$url64          = 'https://github.com/punkscience/derpy/releases/download/v1.0.0/derpy_1.0.0_windows_amd64.zip'
$url64arm       = 'https://github.com/punkscience/derpy/releases/download/v1.0.0/derpy_1.0.0_windows_arm64.zip'
$checksum64     = '7fcea3cf8565b56879e6a00b90956b6ff01e57de8ca7b5bf01235d0430e09873'
$checksum64arm  = 'a03193b0e74a3ec4ca580049a85d114c55a26dab39ae5f5a096a59f49e4f3041'
$checksumType   = 'sha256'

# Select arch from the environment (fast, no WMI dependency).
if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') {
    $url        = $url64arm
    $checksum   = $checksum64arm
} else {
    $url        = $url64
    $checksum   = $checksum64
}

$toolsDir = "$(Split-Path -Parent $MyInvocation.MyCommand.Definition)"

$packageArgs = @{
    packageName   = $packageName
    unzipLocation = $toolsDir
    url64bit      = $url
    checksum64    = $checksum
    checksumType64= $checksumType
}

Install-ChocolateyZipPackage @packageArgs