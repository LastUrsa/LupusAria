# Run only on a disposable GitHub-hosted Windows runner. This installs and removes
# the application and its HKLM uninstall registration, but never starts the bot.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Installer,
    [string]$PreviousInstaller = '',
    [string]$Executable = './build/bin/LupusAria.exe'
)
$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_OS -ne 'Windows') {
    throw 'Use a disposable GitHub Actions Windows runner for installer smoke tests.'
}
$Installer = (Resolve-Path -LiteralPath $Installer).Path
$Executable = (Resolve-Path -LiteralPath $Executable).Path
if ($PreviousInstaller) { $PreviousInstaller = (Resolve-Path -LiteralPath $PreviousInstaller).Path }
$smokeRoot = Join-Path $env:RUNNER_TEMP ('lupusaria-smoke-' + [guid]::NewGuid().ToString('N'))
$installDir = Join-Path $smokeRoot 'Installed App'
$stateDir = Join-Path $smokeRoot 'User State'
$reportPath = Join-Path $env:RUNNER_TEMP 'windows-smoke-results.json'
$results = [System.Collections.Generic.List[object]]::new()
$originalEnvPath = $env:LUPUSARIA_ENV_PATH
$originalAppData = $env:APPDATA
$originalState = $env:LUPUSARIA_SMOKE_STATE_DIR
$originalStage = $env:LUPUSARIA_SMOKE_STAGE
$process = $null
function Record-Pass([string]$Name) {
    $results.Add(@{ check = $Name; status = 'passed' })
    Write-Host "PASS: $Name"
}
function Invoke-CheckedProcess([string]$File, [string[]]$Arguments) {
    $child = Start-Process -FilePath $File -ArgumentList $Arguments -PassThru
    if (-not $child.WaitForExit(120000)) { Stop-Process -Id $child.Id -Force; throw "Process timed out: $File" }
    if ($child.ExitCode -ne 0) { throw "Process failed ($($child.ExitCode)): $File" }
}
function Verify-State {
    $env:LUPUSARIA_SMOKE_STAGE = 'verify'
    & go test ./internal/rehearsal -run '^TestInstallerPersistenceFixture$' -count=1 -v
    if ($LASTEXITCODE -ne 0) { throw 'Persisted raffle verification failed' }
}
function Test-Launch([string]$Name) {
    # Require a clean port before launch: another process cannot satisfy readiness.
    $probe = [System.Net.Sockets.TcpClient]::new()
    try { $probe.Connect('127.0.0.1', 47831); throw 'Overlay port already in use before app launch' }
    catch [System.Net.Sockets.SocketException] { }
    finally { $probe.Dispose() }
    $script:process = Start-Process -FilePath (Join-Path $installDir 'LupusAria.exe') -WorkingDirectory $installDir -PassThru
    $ready = $false
    $deadline = [DateTime]::UtcNow.AddSeconds(45)
    while ([DateTime]::UtcNow -lt $deadline) {
        $script:process.Refresh()
        if ($script:process.HasExited) { throw "Application exited during $Name (code $($script:process.ExitCode))" }
        try {
            $response = Invoke-WebRequest -Uri 'http://127.0.0.1:47831/' -TimeoutSec 2
            if ($response.StatusCode -eq 200 -and $response.Content -match 'html') { $ready = $true; break }
        } catch { Start-Sleep -Milliseconds 250 }
    }
    if (-not $ready) { throw "Application startup did not expose its overlay during $Name" }
    Record-Pass "$Name process and backend startup"
    # Closing a window may be unsupported in the runner session. Report that
    # distinction; neither route claims interactive UI rendering validation.
    $graceful = $script:process.CloseMainWindow()
    if (-not $graceful -or -not $script:process.WaitForExit(10000)) {
        Stop-Process -Id $script:process.Id -Force
        $script:process.WaitForExit()
        Record-Pass "$Name forced termination completed"
    } else { Record-Pass "$Name window-close shutdown completed" }
    $script:process = $null
    Verify-State
    Record-Pass "$Name retained committed raffle entries and draw"
}
try {
    New-Item -ItemType Directory -Path $stateDir -Force | Out-Null
    $env:APPDATA = Join-Path $smokeRoot 'AppData'
    New-Item -ItemType Directory -Path $env:APPDATA -Force | Out-Null
    $env:LUPUSARIA_ENV_PATH = Join-Path $stateDir '.env'
    Set-Content -LiteralPath $env:LUPUSARIA_ENV_PATH -Value "AI_PROVIDER=mock`nRAFFLE_ENABLED=true" -Encoding utf8NoBOM
    $env:LUPUSARIA_SMOKE_STATE_DIR = $stateDir
    $env:LUPUSARIA_SMOKE_STAGE = 'seed'
    & go test ./internal/rehearsal -run '^TestInstallerPersistenceFixture$' -count=1 -v
    if ($LASTEXITCODE -ne 0) { throw 'Could not seed isolated raffle state' }
    Record-Pass 'Isolated real-SQLite fixture created'
    $configHash = (Get-FileHash -LiteralPath $env:LUPUSARIA_ENV_PATH).Hash
    $firstInstaller = if ($PreviousInstaller) { $PreviousInstaller } else { $Installer }
    Invoke-CheckedProcess $firstInstaller @('/S', "/D=$installDir")
    if (-not (Test-Path -LiteralPath (Join-Path $installDir 'LupusAria.exe'))) { throw 'Installed executable missing' }
    Record-Pass 'Silent installation'
    Test-Launch 'Installed app'
    Invoke-CheckedProcess $Installer @('/S', "/D=$installDir")
    Record-Pass $(if ($PreviousInstaller) { 'Cross-version upgrade installation' } else { 'Same-candidate reinstall (cross-version upgrade not tested)' })
    $installedHash = (Get-FileHash -LiteralPath (Join-Path $installDir 'LupusAria.exe')).Hash
    if ($installedHash -ne (Get-FileHash -LiteralPath $Executable).Hash) { throw 'Installed candidate differs from built executable' }
    Record-Pass 'Installed candidate matches built artifact checksum'
    Verify-State
    Test-Launch 'Reinstalled app' 
    if ((Get-FileHash -LiteralPath $env:LUPUSARIA_ENV_PATH).Hash -ne $configHash) { throw 'Installer or startup changed configuration' }
    Record-Pass 'Configuration preserved'
    $uninstaller = Join-Path $installDir 'uninstall.exe'
    if (-not (Test-Path -LiteralPath $uninstaller)) { throw 'Uninstaller missing' }
    Invoke-CheckedProcess $uninstaller @('/S', "_?=$installDir")
    if (Test-Path -LiteralPath (Join-Path $installDir 'LupusAria.exe')) { throw 'Uninstall left executable behind' }
    Verify-State
    Record-Pass 'Silent uninstall preserved external user data'
} catch {
    $results.Add(@{ check = 'Windows smoke'; status = 'failed'; error = $_.Exception.Message })
    throw
} finally {
    if ($process -and -not $process.HasExited) { Stop-Process -Id $process.Id -Force }
    $results | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $reportPath -Encoding utf8
    $env:LUPUSARIA_ENV_PATH = $originalEnvPath
    $env:APPDATA = $originalAppData
    $env:LUPUSARIA_SMOKE_STATE_DIR = $originalState
    $env:LUPUSARIA_SMOKE_STAGE = $originalStage
    Write-Host "Smoke report: $reportPath"
}
