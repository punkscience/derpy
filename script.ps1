param (
    [Parameter(Mandatory = $true)]
    [string]$TargetPath
)

if (-not (Test-Path $TargetPath)) {
    Write-Error "The path '$TargetPath' does not exist."
    exit 1
}

function Fix-Path {
    param (
        [string]$path
    )

    if ([string]::IsNullOrWhiteSpace($path)) { return $null }

    $segments = $path.Split([char]'\')
    $fixedSegments = $segments | ForEach-Object { $_.Trim() }
    return ($fixedSegments -join '\')
}

function Rename-ItemSafely {
    param (
        [System.IO.FileSystemInfo]$item
    )

    $originalPath = $item.FullName
    $fixedPath = Fix-Path $originalPath

    if ($originalPath -ne $fixedPath -and -not [string]::IsNullOrWhiteSpace($fixedPath)) {
        try {
            $destination = [System.IO.Path]::GetFullPath($fixedPath)
            Move-Item -LiteralPath $originalPath -Destination $destination
            Write-Host "Renamed:`n$originalPath`n→ $destination`n"
        } catch {
            Write-Warning "Failed to rename:`n$originalPath`n→ $destination`n$($_.Exception.Message)"
        }
    }
}

# Rename folders first (deepest first)
try {
    Get-ChildItem -Path $TargetPath -Recurse -Directory -Force -ErrorAction Stop |
    Sort-Object -Property FullName -Descending |
    ForEach-Object { Rename-ItemSafely $_ }
} catch {
    Write-Warning "Folder scan failed: $($_.Exception.Message)"
}

# Then rename files
try {
    Get-ChildItem -Path $TargetPath -Recurse -File -Force -ErrorAction Stop |
    ForEach-Object { Rename-ItemSafely $_ }
} catch {
    Write-Warning "File scan failed: $($_.Exception.Message)"
}