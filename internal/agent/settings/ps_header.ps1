# Cabecera común de los ajustes de Windows del agente MDM.
# MDM_MODE es «Test» (comprobar) o «Set» (aplicar). Los parámetros llegan en
# variables MDM_PARAM_<NOMBRE>. La salida es una única línea JSON.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$Mode = $env:MDM_MODE

function Write-Result([bool]$Compliant, [string]$Detail, [bool]$NotApplicable = $false, $Recovery = $null) {
    [pscustomobject]@{ compliant = $Compliant; detail = $Detail; not_applicable = $NotApplicable; recovery = $Recovery } |
        ConvertTo-Json -Compress -Depth 4
}

function Write-Done([string]$Detail = 'Aplicado.') {
    [pscustomobject]@{ compliant = $true; detail = $Detail } | ConvertTo-Json -Compress
}

function Get-Param([string]$Name, $Default = $null) {
    $v = [Environment]::GetEnvironmentVariable('MDM_PARAM_' + $Name)
    if ($null -eq $v -or $v -eq '') { return $Default }
    return $v
}

function Test-Flag([string]$Name, [bool]$Default) {
    $v = Get-Param $Name
    if ($null -eq $v) { return $Default }
    return $v -eq 'true'
}

function Set-RegistryDword([string]$Path, [string]$Name, [int]$Value) {
    if (-not (Test-Path $Path)) { New-Item -Path $Path -Force | Out-Null }
    New-ItemProperty -Path $Path -Name $Name -Value $Value -PropertyType DWord -Force | Out-Null
}

function Get-RegistryValue([string]$Path, [string]$Name) {
    $item = Get-ItemProperty -Path $Path -Name $Name -ErrorAction SilentlyContinue
    if ($null -eq $item) { return $null }
    return $item.$Name
}

try {
