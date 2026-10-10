param([Parameter(Mandatory = $true)][string]$OutputPath)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.IO.Compression.FileSystem
$sourceRoot = Split-Path -Parent $PSScriptRoot
$destination = [IO.Path]::GetFullPath($OutputPath)
if (Test-Path -LiteralPath $destination) { throw "Output already exists: $destination" }
$files = @()
foreach ($folder in @('cmd', 'configs', 'deploy', 'internal', 'scripts', 'web')) {
    $files += Get-ChildItem -LiteralPath (Join-Path $sourceRoot $folder) -File -Recurse -Force | Where-Object {
        $relative = $_.FullName.Substring($sourceRoot.Length + 1).Replace('\', '/')
        # Only exclude known generated directories; src/mock/data is source code.
        $relative -notmatch '^(web/node_modules/|web/dist/|internal/web/dist/)' -and
        $relative -ne 'deploy/config.yaml' -and
        $_.Name -notlike '.env*' -and
        ($folder -ne 'configs' -or $_.Name -like '*.example')
    }
}
$files += Get-ChildItem -LiteralPath $sourceRoot -File -Force | Where-Object {
    $_.Name -in @('.dockerignore', '.gitignore', 'go.mod', 'go.sum', 'Makefile') -or $_.Extension -eq '.md'
}
$archive = [IO.Compression.ZipFile]::Open($destination, [IO.Compression.ZipArchiveMode]::Create)
try {
    foreach ($file in $files) {
        $relative = $file.FullName.Substring($sourceRoot.Length + 1).Replace('\', '/')
        [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($archive, $file.FullName, $relative, [IO.Compression.CompressionLevel]::Optimal) | Out-Null
    }
} finally { $archive.Dispose() }
$archive = [IO.Compression.ZipFile]::OpenRead($destination)
try {
    if ($archive.Entries.Count -ne $files.Count) { throw 'Archive file count mismatch' }
    foreach ($file in $files) {
        $relative = $file.FullName.Substring($sourceRoot.Length + 1).Replace('\', '/')
        $entry = $archive.GetEntry($relative)
        if ($null -eq $entry) { throw "Missing source file: $relative" }
        $stream = $entry.Open()
        $hasher = [Security.Cryptography.SHA256]::Create()
        try { $actual = [BitConverter]::ToString($hasher.ComputeHash($stream)).Replace('-', '') }
        finally { $stream.Dispose(); $hasher.Dispose() }
        if ($actual -ne (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash) { throw "Content mismatch: $relative" }
    }
    if ($null -eq $archive.GetEntry('web/src/mock/data/recycle.ts')) { throw 'Missing recycle mock source' }
    if ($null -ne $archive.GetEntry('deploy/config.yaml')) { throw 'Runtime config must not be packaged' }
} finally { $archive.Dispose() }
Get-Item -LiteralPath $destination | Select-Object FullName, Length
Get-FileHash -LiteralPath $destination -Algorithm SHA256
Write-Output "Verified $($files.Count) files against source SHA256 hashes."
