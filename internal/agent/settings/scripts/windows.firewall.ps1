    if ($Mode -eq 'Set') {
        Set-NetFirewallProfile -Profile Domain, Private, Public -Enabled True
        Write-Done
        return
    }
    $off = @(Get-NetFirewallProfile | Where-Object { -not $_.Enabled } | ForEach-Object { $_.Name })
    if ($off.Count -eq 0) {
        Write-Result $true 'Activado en los tres perfiles.'
    }
    else {
        Write-Result $false ('Desactivado en: ' + ($off -join ', ') + '.')
    }
