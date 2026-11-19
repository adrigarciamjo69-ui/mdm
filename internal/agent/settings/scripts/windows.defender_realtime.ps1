    if (-not (Get-Command Get-MpComputerStatus -ErrorAction SilentlyContinue)) {
        Write-Result $false 'Microsoft Defender no está instalado.' $true
        return
    }
    $status = Get-MpComputerStatus
    if ($status.AMRunningMode -and $status.AMRunningMode -ne 'Normal') {
        Write-Result $false ("Defender no es el antivirus activo (modo: $($status.AMRunningMode)).") $true
        return
    }
    if ($Mode -eq 'Set') {
        Set-MpPreference -DisableRealtimeMonitoring $false
        Write-Done
        return
    }
    if ($status.RealTimeProtectionEnabled) {
        Write-Result $true 'Protección en tiempo real activada.'
    }
    else {
        Write-Result $false 'Protección en tiempo real desactivada.'
    }
