param (
    [Parameter(Mandatory = $true)]
    [string]$TargetPath
)

if (-not (Test-Path $TargetPath)) {
    Write-Error "The path '$TargetPath' does not exist."
    exit 1
}

function Fix-PathSegments {
    param (
        [string]$fullPath
    )

    $segments = $fullPath -split [System.IO.Path]::DirectorySeparatorChar
    $fixedSegments = @()

    foreach ($segment in $segments) {
        $fixedSegments += $segment.Trim()
    }

    return ($fixedSegments -join [System.IO.Path]::DirectorySeparatorChar)
}

function Rename-IfNeeded {
    param (
        [System.IO.FileSystemInfo]$item
    )

    $originalPath = $item.FullName
    $fixedPath = Fix-PathSegments $originalPath

    if ($originalPath -ne $fixedPath) {
        try {
            $parent = Split-Path -Path $fixedPath -Parent
            if (-not (Test-Path $parent)) {
                New-Item -ItemType Directory -Path $parent -Force | Out-Null
            }

            Move-Item -LiteralPath $originalPath -Destination $fixedPath
            Write-Host "Renamed:`n$originalPath`n→ $fixedPath`n"
        } catch {
            Write-Warning "Failed to rename:`n$originalPath`n→ $fixedPath`n$($_.Exception.Message)"
        }
    }
}

# Rename folders first (deepest first)
Get-ChildItem -Path $TargetPath -Recurse -Directory -Force |
Sort-Object -Property FullName -Descending |
ForEach-Object {
    Rename-IfNeeded $_
}

# Then rename files
Get-ChildItem -Path $TargetPath -Recurse -File -Force |
ForEach-Object {
    Rename-IfNeeded $_
}