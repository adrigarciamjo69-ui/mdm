<#
.SYNOPSIS
    Instala el agente MDM en un equipo Windows y lo inscribe en el servidor.

.EXAMPLE
    powershell -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create((irm https://servidor/downloads/install.ps1))) -Server https://servidor -Token <token>"

.PARAMETER Server
    Dirección del servidor (https://…).

.PARAMETER Token
    Token de inscripción creado en la consola.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Server,
    [Parameter(Mandatory = $true)][string]$Token
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Fail([string]$Message, [int]$Code = 1) {
    [Console]::Error.WriteLine("Error: $Message")
    exit $Code
}

if ($Server -notmatch '^https://') {
    Fail 'la dirección del servidor debe empezar por https://'
}
$Server = $Server.TrimEnd('/')

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Fail 'hace falta ejecutar como administrador. Abre PowerShell con «Ejecutar como administrador».'
}

# PROCESSOR_ARCHITEW6432 existe cuando un proceso de 32 bits corre en un sistema de 64.
$procArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
switch ($procArch) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { Fail "Arquitectura no admitida: $procArch. El agente está disponible para amd64 y arm64." }
}

[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$name = "mdm-agent-windows-$arch.exe"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("mdm-agent-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
$exe = Join-Path $tmp 'mdm-agent.exe'
$sums = Join-Path $tmp 'SHA256SUMS'
$code = 1

try {
    Write-Host "Descargando el agente ($arch) de $Server…"
    try {
        Invoke-WebRequest -UseBasicParsing -Uri "$Server/downloads/$name" -OutFile $exe
        Invoke-WebRequest -UseBasicParsing -Uri "$Server/downloads/SHA256SUMS" -OutFile $sums
    }
    catch {
        Fail "No se ha podido descargar el agente de $Server. Comprueba la dirección, la red y el certificado. Detalle: $($_.Exception.Message)" 3
    }

    # Cada línea de SHA256SUMS tiene la forma «<suma>  <nombre>».
    $expected = $null
    foreach ($line in Get-Content -Path $sums) {
        $parts = $line -split '\s+', 2
        if ($parts.Count -eq 2 -and $parts[1].TrimStart('*') -eq $name) {
            $expected = $parts[0].ToLowerInvariant()
            break
        }
    }
    if (-not $expected) {
        Fail "el servidor no publica la suma de comprobación de $name"
    }
    $actual = (Get-FileHash -Path $exe -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($expected -ne $actual) {
        Fail 'La suma de comprobación no coincide: la descarga está dañada o ha sido manipulada. No se ha instalado nada.'
    }

    # enroll copia el ejecutable a su ubicación definitiva e instala el servicio;
    # su código de salida es el de este script (2: token rechazado, 3: fallo de red).
    & $exe enroll --server $Server --token $Token
    $code = $LASTEXITCODE
}
finally {
    Remove-Item -Recurse -Force -Path $tmp -ErrorAction SilentlyContinue
}
exit $code
