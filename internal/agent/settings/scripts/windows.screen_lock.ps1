    $path = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System'
    $seconds = [int](Get-Param 'MINUTES' 10) * 60
    if ($Mode -eq 'Set') {
        Set-RegistryDword $path 'InactivityTimeoutSecs' $seconds
        Write-Done
        return
    }
    $current = Get-RegistryValue $path 'InactivityTimeoutSecs'
    if ($null -ne $current -and $current -gt 0 -and $current -le $seconds) {
        Write-Result $true "Bloqueo tras $current s de inactividad."
    }
    elseif ($null -eq $current -or $current -eq 0) {
        Write-Result $false 'No hay bloqueo por inactividad configurado.'
    }
    else {
        Write-Result $false "El bloqueo está a $current s; el máximo es $seconds s."
    }
