# Download only this repository's published Windows baseline and verify its hash.
$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_OS -ne 'Windows') {
    throw 'This baseline helper is intended for the disposable Windows CI runner.'
}
$baselineDir = Join-Path $env:RUNNER_TEMP ('lupusaria-baseline-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $baselineDir -Force | Out-Null
$metadata = & gh release view --repo $env:GITHUB_REPOSITORY --json tagName
if ($LASTEXITCODE -ne 0) { throw 'Could not resolve the previous published release' }
$tag = ($metadata | ConvertFrom-Json).tagName
& gh release download $tag --repo $env:GITHUB_REPOSITORY --pattern '*-windows-amd64-installer.exe' --pattern 'SHA256SUMS.txt' --dir $baselineDir
if ($LASTEXITCODE -ne 0) { throw 'Could not download the previous release installer and checksums' }
$installers = @(Get-ChildItem -LiteralPath $baselineDir -Filter '*-windows-amd64-installer.exe')
if ($installers.Count -ne 1) { throw 'Expected exactly one previous Windows amd64 installer' }
$installer = $installers[0]
$expected = $null
foreach ($line in Get-Content -LiteralPath (Join-Path $baselineDir 'SHA256SUMS.txt')) {
    if ($line -match ('^([a-fA-F0-9]{64})\s+\*?' + [regex]::Escape($installer.Name) + '$')) {
        $expected = $Matches[1]
    }
}
if (-not $expected -or (Get-FileHash -LiteralPath $installer.FullName -Algorithm SHA256).Hash -ne $expected) {
    throw 'Published baseline installer checksum did not verify'
}
Write-Host "Verified upgrade baseline: $tag / $($installer.Name)"
"installer=$($installer.FullName)" | Out-File -FilePath $env:GITHUB_OUTPUT -Append -Encoding utf8
"tag=$tag" | Out-File -FilePath $env:GITHUB_OUTPUT -Append -Encoding utf8
