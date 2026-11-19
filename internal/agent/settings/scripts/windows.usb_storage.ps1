    $path = 'HKLM:\SYSTEM\CurrentControlSet\Services\USBSTOR'
    if (-not (Test-Path $path)) {
        Write-Result $false 'El controlador de almacenamiento USB no está instalado.' $true
        return
    }
    $blocked = Test-Flag 'BLOCKED' $true
    $want = if ($blocked) { 4 } else { 3 }
    if ($Mode -eq 'Set') {
        Set-RegistryDword $path 'Start' $want
        Write-Done
        return
    }
    $current = Get-RegistryValue $path 'Start'
    $state = if ($current -eq 4) { 'bloqueado' } else { 'permitido' }
    Write-Result (($current -eq 4) -eq $blocked) "Almacenamiento USB $state."
