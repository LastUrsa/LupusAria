param([string] $Executable = "./build/bin/LupusAria.exe")

$ErrorActionPreference = "Stop"
$Executable = (Resolve-Path $Executable).Path
$isolated = Join-Path ([System.IO.Path]::GetTempPath()) ("lupusaria-sip-" + [guid]::NewGuid())
New-Item -Path $isolated -ItemType Directory | Out-Null
$previous = $env:LUPUSARIA_ENV_PATH
$previousAppData = $env:APPDATA
$env:APPDATA = $isolated
$env:LUPUSARIA_ENV_PATH = Join-Path $isolated ".env"
New-Item -Path $env:LUPUSARIA_ENV_PATH -ItemType File | Out-Null
$process = $null

try {
    $process = Start-Process -FilePath $Executable -ArgumentList "--service" -PassThru
    $baseUrl = "http://127.0.0.1:47050"
    $ready = $false
    for ($i = 0; $i -lt 75; $i++) {
        $process.Refresh()
        if ($process.HasExited) { throw "LupusAria service exited early" }
        try {
            $app = Invoke-RestMethod -Uri "$baseUrl/api/v1/app" -TimeoutSec 1
            if ($app.appId -eq "lupusaria" -and $app.mode -eq "service") { $ready = $true; break }
        } catch { }
        Start-Sleep -Milliseconds 200
    }
    if (!$ready) { throw "LupusAria SIP did not become ready" }
    $process.Refresh()
    if ($process.MainWindowHandle -ne 0) { throw "Service mode opened a window" }
    npm exec --yes --package=newman@6.2.1 -- newman run docs/postman/LupusAria-SIP-v1.postman_collection.json --env-var "baseUrl=$baseUrl" --timeout-request 35000 --bail
    if ($LASTEXITCODE -ne 0) { throw "Newman failed with exit code $LASTEXITCODE" }
} finally {
    if ($process -and !$process.HasExited) { Stop-Process -Id $process.Id -ErrorAction SilentlyContinue }
    $env:LUPUSARIA_ENV_PATH = $previous
    $env:APPDATA = $previousAppData
    Remove-Item -Path $isolated -Recurse -Force
}
