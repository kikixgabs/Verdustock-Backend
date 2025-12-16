param(
    [string]$Username,
    [string]$Email,
    [string]$Password,
    [ValidateSet("Dev", "Prod")] 
    [string]$TargetEnv = "Dev"  # Por defecto usa Dev (Localhost)
)

# 1. Configuración dinámica según el entorno elegido
if ($TargetEnv -eq "Prod") {
    Write-Host "🌍 Modo: PRODUCCIÓN (Render)" -ForegroundColor Cyan
    $BaseUrl = "https://verdustock-backend.onrender.com"
    $EnvFileName = ".env.production"
} else {
    Write-Host "💻 Modo: DESARROLLO (Localhost)" -ForegroundColor Yellow
    $BaseUrl = "http://localhost:10000" # Ajustado a tu puerto local
    $EnvFileName = ".env.development"
}

# 2. Función para leer variables (ahora acepta el nombre del archivo)
function Get-EnvValue {
    param($Key, $File)
    $envFilePath = "$PSScriptRoot\$File"
    
    if (-not (Test-Path $envFilePath)) {
        Write-Error "El archivo '$File' no existe. Asegúrate de tenerlo creado localmente con la ADMIN_SECRET_KEY."
        exit 1
    }
    
    $val = Select-String -Path $envFilePath -Pattern "^$Key=(.+)$"
    if ($val) {
        return $val.Matches.Groups[1].Value.Trim()
    }
    return $null
}

# 3. Obtener el secreto del archivo correspondiente
$secret = Get-EnvValue "ADMIN_SECRET_KEY" $EnvFileName

if (-not $secret) {
    Write-Error "ADMIN_SECRET_KEY no encontrada en $EnvFileName"
    exit 1
}

# 4. Pedir datos si faltan
if (-not $Username) { $Username = Read-Host "Enter Username" }
if (-not $Email) { $Email = Read-Host "Enter Email" }
if (-not $Password) { $Password = Read-Host "Enter Password" }

$body = @{
    username = $Username
    email = $Email
    password = $Password
} | ConvertTo-Json

# 5. Ejecutar la petición
try {
    Write-Host "Conectando a: $BaseUrl/admin/create-user..." -ForegroundColor Gray
    
    $response = Invoke-RestMethod -Uri "$BaseUrl/admin/create-user" `
        -Method Post `
        -Headers @{ "Content-Type" = "application/json"; "X-Admin-Secret" = $secret } `
        -Body $body
        
    Write-Host "¡Éxito! Usuario creado en $TargetEnv." -ForegroundColor Green
    $response | Format-List
} catch {
    Write-Host "Error fatal: $_" -ForegroundColor Red
    if ($_.Exception.Response) {
        $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
        Write-Host "Detalle del servidor: $($reader.ReadToEnd())" -ForegroundColor Red
    }
}