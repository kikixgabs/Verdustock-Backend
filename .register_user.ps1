param(
    [string]$Username,
    [string]$Email,
    [string]$Password
)

# Function to read value from .env file
function Get-EnvValue {
    param($Key)
    $envFile = "$PSScriptRoot\.env.development"
    if (-not (Test-Path $envFile)) {
        Write-Error ".env.development file not found!"
        exit 1
    }
    
    $val = Select-String -Path $envFile -Pattern "^$Key=(.+)$"
    if ($val) {
        return $val.Matches.Groups[1].Value.Trim()
    }
    return $null
}

# Get secret from .env
$secret = Get-EnvValue "ADMIN_SECRET_KEY"

if (-not $secret) {
    Write-Error "ADMIN_SECRET_KEY not found in .env.development"
    exit 1
}

# Interactive prompts if args are missing
if (-not $Username) { $Username = Read-Host "Enter Username" }
if (-not $Email) { $Email = Read-Host "Enter Email" }
if (-not $Password) { $Password = Read-Host "Enter Password" }

$body = @{
    username = $Username
    email = $Email
    password = $Password
} | ConvertTo-Json

try {
    $response = Invoke-RestMethod -Uri "http://localhost:8080/admin/create-user" `
        -Method Post `
        -Headers @{ "Content-Type" = "application/json"; "X-Admin-Secret" = $secret } `
        -Body $body
        
    Write-Host "Success!" -ForegroundColor Green
    $response | Format-List
} catch {
    Write-Host "Error: $_" -ForegroundColor Red
    if ($_.Exception.Response) {
        $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
        Write-Host $reader.ReadToEnd() -ForegroundColor Red
    }
}
